package compile

import (
	"encoding/json"
	"strconv"
	"strings"

	"panel/internal/agent/hysteria"
	"panel/internal/panel/routing"
	"panel/internal/panel/store"
)

// Hysteria builds the state for the Hysteria adapter: one server per enabled
// hysteria2 inbound and the password table for its auth endpoint. It returns
// nil when there is nothing to serve.
func Hysteria(inbounds []store.Inbound, users []store.User, rt Routing) ([]byte, error) {
	st := hysteria.State{Users: map[string]string{}}
	acl, outbounds, resolver := hysteriaRouting(rt)
	for _, ib := range inbounds {
		if !ib.Enabled || ib.Protocol != "hysteria2" || ib.Settings.Hysteria == nil {
			continue
		}
		h := ib.Settings.Hysteria
		server := map[string]any{
			"listen": ":" + strconv.Itoa(ib.Port),
			// Anyone who is not a Hysteria client (scanners, browsers over
			// HTTP/3) sees this real site.
			"masquerade": map[string]any{
				"type":  "proxy",
				"proxy": map[string]any{"url": h.Masquerade, "rewriteHost": true},
			},
			"acl":       acl,
			"outbounds": outbounds,
		}
		if resolver != nil {
			server["resolver"] = resolver
		}
		if h.ObfsPassword != "" {
			server["obfs"] = map[string]any{
				"type":       "salamander",
				"salamander": map[string]any{"password": h.ObfsPassword},
			}
		}
		if h.UpMbps > 0 || h.DownMbps > 0 {
			bw := map[string]any{}
			if h.UpMbps > 0 {
				bw["up"] = strconv.Itoa(h.UpMbps) + " mbps"
			}
			if h.DownMbps > 0 {
				bw["down"] = strconv.Itoa(h.DownMbps) + " mbps"
			}
			server["bandwidth"] = bw
		}
		st.Instances = append(st.Instances, hysteria.Instance{
			Tag:     "in-" + strconv.FormatInt(ib.ID, 10),
			Server:  server,
			CertPEM: h.CertPEM,
			KeyPEM:  h.KeyPEM,
		})
	}
	if len(st.Instances) == 0 {
		return nil, nil
	}
	for _, u := range users {
		st.Users[u.UUID] = Email(u.ID)
	}
	return json.MarshalIndent(st, "", "  ")
}

// hysteriaRouting translates the rules into Hysteria's ACL. Hysteria has no
// notion of users or protocols, so rules that depend on them are skipped
// (Rule.Support tells the UI), and so are keyword and regexp entries.
func hysteriaRouting(rt Routing) (acl map[string]any, outbounds []any, resolver map[string]any) {
	mode := "46"
	switch rt.DNS.Strategy {
	case routing.OnlyIPv4:
		mode = "4"
	case routing.BothIPs:
		mode = "auto"
	}
	// The first outbound takes whatever the ACL does not match.
	outbounds = []any{
		map[string]any{"name": routing.Direct, "type": "direct", "direct": map[string]any{"mode": mode}},
		map[string]any{"name": routing.IPv4, "type": "direct", "direct": map[string]any{"mode": "4"}},
	}
	warp := rt.usesWarp() && rt.Bridge.Port > 0
	if warp {
		outbounds = append(outbounds, map[string]any{"name": routing.WARP, "type": "socks5", "socks5": map[string]any{
			"addr": "127.0.0.1:" + strconv.Itoa(rt.Bridge.Port), "username": rt.Bridge.User, "password": rt.Bridge.Password,
		}})
	}
	var lines []string
	usesGeo := false
	for _, r := range rt.Rules {
		if len(r.Users) > 0 || len(r.Protocols) > 0 {
			continue
		}
		out := r.Outbound
		if out == routing.Block {
			out = "reject"
		}
		if out == routing.WARP && !warp {
			out = routing.Direct
		}
		var addrs []string
		domains, ips := split(r.Match)
		for _, m := range append(domains, ips...) {
			switch {
			case strings.HasPrefix(m, "domain:"):
				addrs = append(addrs, "suffix:"+strings.TrimPrefix(m, "domain:"))
			case strings.HasPrefix(m, "full:"):
				addrs = append(addrs, strings.TrimPrefix(m, "full:"))
			case strings.HasPrefix(m, "geosite:") || strings.HasPrefix(m, "geoip:"):
				if !strings.Contains(m, "@") {
					addrs = append(addrs, m)
					usesGeo = true
				}
			case strings.HasPrefix(m, "keyword:") || strings.HasPrefix(m, "regexp:"):
			default:
				addrs = append(addrs, m)
			}
		}
		if len(r.Match) == 0 {
			addrs = []string{"all"}
		}
		if len(addrs) == 0 {
			continue // nothing Hysteria can match; never widen to "all"
		}
		for _, a := range addrs {
			for _, pp := range hysteriaPorts(r.Network, r.Ports) {
				if pp == "" {
					lines = append(lines, out+"("+a+")")
				} else {
					lines = append(lines, out+"("+a+", "+pp+")")
				}
			}
		}
	}
	if rt.Final == routing.WARP && warp {
		lines = append(lines, "warp(all)")
	}
	acl = map[string]any{"inline": lines}
	if usesGeo && rt.GeoDir != "" {
		acl["geoip"] = rt.GeoDir + "/geoip.dat"
		acl["geosite"] = rt.GeoDir + "/geosite.dat"
	}
	if srv, ok := rt.DNS.Server(); ok {
		if srv.URL != "" {
			addr := srv.Addr
			if addr == "" {
				addr = srv.Host + ":443"
			}
			resolver = map[string]any{"type": "https", "https": map[string]any{"addr": addr, "sni": strings.Split(srv.Host, ":")[0], "timeout": "5s"}}
		} else {
			resolver = map[string]any{"type": "udp", "udp": map[string]any{"addr": srv.Addr, "timeout": "5s"}}
		}
	}
	return acl, outbounds, resolver
}

// hysteriaPorts gives the "proto/port" parts of ACL lines; "" means any.
func hysteriaPorts(network, ports string) []string {
	protos := []string{"tcp", "udp"}
	if network != "" {
		protos = []string{network}
	}
	ranges := portRanges(ports)
	if len(ranges) == 0 {
		if network == "" {
			return []string{""}
		}
		return []string{network}
	}
	var out []string
	for _, p := range protos {
		for _, r := range ranges {
			if r[0] == r[1] {
				out = append(out, p+"/"+strconv.Itoa(r[0]))
			} else {
				out = append(out, p+"/"+strconv.Itoa(r[0])+"-"+strconv.Itoa(r[1]))
			}
		}
	}
	return out
}
