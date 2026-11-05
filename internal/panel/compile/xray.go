// Package compile turns the panel's stored intent (inbounds, users, later
// routing and DNS) into the final config of each core. It is pure: same input,
// byte-identical output, so the agent can skip restarts when nothing changed.
package compile

import (
	"encoding/json"
	"net"
	"strconv"

	"panel/internal/panel/routing"
	"panel/internal/panel/store"
)

// Email is the per-user tag cores report traffic under. It uses the stable
// id, not the editable name.
func Email(userID int64) string { return "u" + strconv.FormatInt(userID, 10) }

// UserIDFromEmail reverses Email.
func UserIDFromEmail(email string) (int64, bool) {
	if len(email) < 2 || email[0] != 'u' {
		return 0, false
	}
	id, err := strconv.ParseInt(email[1:], 10, 64)
	return id, err == nil && id > 0
}

// Private and special ranges that clients must never reach through the
// VPN (the provider's internal network, the server itself, metadata APIs).
var privateCIDRs = []string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.168.0.0/16", "198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4",
	"::1/128", "fc00::/7", "fe80::/10", "ff00::/8",
}

// xrayProtocols are the inbound protocols served by Xray.
var xrayProtocols = map[string]bool{"vless": true, "trojan": true, "vmess": true, "shadowsocks": true}

// Xray builds the Xray config for the enabled inbounds and active users.
// It returns nil when there is nothing to serve, meaning "stop the core".
// With WARP in use Xray also carries the loopback bridge other cores reach
// WARP through, so it runs even without inbounds of its own.
func Xray(inbounds []store.Inbound, users []store.User, apiAddr string, rt Routing) ([]byte, error) {
	var ins []any
	for _, ib := range inbounds {
		if ib.Enabled && xrayProtocols[ib.Protocol] {
			ins = append(ins, xrayInbound(ib, users))
		}
	}
	// Скрытые подключения для трафика с других наших серверов (каскад).
	for _, e := range rt.Cascades {
		ins = append(ins, cascadeInbound(e))
	}
	// The bridge runs whenever WARP is registered and on, even before any rule
	// uses it: the WARP check goes through it.
	bridge := rt.Warp != nil && rt.Bridge.Port > 0
	if len(ins) == 0 && !bridge && len(rt.Custom) == 0 {
		return nil, nil
	}
	var rules []any
	if bridge {
		ins = append(ins, map[string]any{
			"tag": "warp-bridge", "listen": "127.0.0.1", "port": rt.Bridge.Port, "protocol": "socks",
			"settings": map[string]any{
				"auth":     "password",
				"accounts": []any{map[string]any{"user": rt.Bridge.User, "pass": rt.Bridge.Password}},
				"udp":      true,
			},
		})
		rules = append(rules, map[string]any{"inboundTag": []string{"warp-bridge"}, "outboundTag": routing.WARP})
	}
	for _, c := range rt.Custom {
		if c.Bridge.Port == 0 {
			continue
		}
		in := "bridge_" + c.Tag
		ins = append(ins, map[string]any{
			"tag": in, "listen": "127.0.0.1", "port": c.Bridge.Port, "protocol": "socks",
			"settings": map[string]any{
				"auth":     "password",
				"accounts": []any{map[string]any{"user": c.Bridge.User, "pass": c.Bridge.Password}},
				"udp":      true,
			},
		})
		rules = append(rules, map[string]any{"inboundTag": []string{in}, "outboundTag": c.Tag})
	}
	needIP := false
	for _, r := range rt.Rules {
		xr, usesIP := xrayRules(r)
		rules = append(rules, xr...)
		needIP = needIP || usesIP
	}
	domainStrategy := "AsIs"
	if needIP {
		// Let geoip and CIDR rules see the address of a domain request.
		domainStrategy = "IPIfNonMatch"
	}
	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning", "access": "none"},
		"api": map[string]any{
			"tag":      "api",
			"listen":   apiAddr,
			"services": []string{"StatsService"},
		},
		"stats": map[string]any{},
		"policy": map[string]any{
			"levels": map[string]any{
				"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true, "statsUserOnline": true},
			},
		},
		"inbounds":  ins,
		"outbounds": xrayOutbounds(rt),
		"routing":   map[string]any{"domainStrategy": domainStrategy, "rules": rules},
	}
	if dns := xrayDNS(rt.DNS); dns != nil {
		cfg["dns"] = dns
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// xrayOutbounds lists the outbounds with the final one first: Xray sends
// whatever no rule matched to the first outbound.
func xrayOutbounds(rt Routing) []any {
	direct := map[string]any{"tag": routing.Direct, "protocol": "freedom"}
	if s := freedomStrategy(rt.DNS); s != "" {
		direct["streamSettings"] = map[string]any{"sockopt": map[string]any{"domainStrategy": s}}
	}
	outs := []any{
		direct,
		map[string]any{"tag": routing.Block, "protocol": "blackhole"},
		map[string]any{"tag": routing.IPv4, "protocol": "freedom", "streamSettings": map[string]any{"sockopt": map[string]any{"domainStrategy": "UseIPv4"}}},
	}
	if w := rt.Warp; w != nil {
		addrs := []string{}
		for _, a := range []string{w.Address4, w.Address6} {
			if a != "" {
				addrs = append(addrs, a)
			}
		}
		strategy := "ForceIPv4v6"
		if rt.DNS.Strategy == routing.OnlyIPv4 {
			strategy = "ForceIPv4"
		}
		outs = append(outs, map[string]any{
			"tag": routing.WARP, "protocol": "wireguard",
			"settings": map[string]any{
				"secretKey": w.PrivateKey,
				"address":   addrs,
				"peers": []any{map[string]any{
					"publicKey": w.PeerPublicKey, "endpoint": warpEndpoint(w.Endpoint),
					// Keeps the session with Cloudflare alive between bursts of traffic.
					"keepAlive":  25,
					"allowedIPs": []string{"0.0.0.0/0", "::/0"},
				}},
				"reserved":       w.Reserved,
				"mtu":            warpMTU,
				"domainStrategy": strategy,
			},
		})
	}
	for _, c := range rt.Custom {
		o := map[string]any{"tag": c.Tag, "protocol": c.Out.Protocol, "settings": c.Out.Settings}
		if c.Out.Stream != nil {
			o["streamSettings"] = c.Out.Stream
		}
		outs = append(outs, o)
	}
	for i, o := range outs {
		if o.(map[string]any)["tag"] == rt.Final {
			outs[0], outs[i] = outs[i], outs[0]
			break
		}
	}
	return outs
}

// freedomStrategy makes the direct outbound resolve with Xray's own DNS
// (and its IPv4/IPv6 preference); with the system resolver it stays AsIs.
func freedomStrategy(d routing.DNSSettings) string {
	if _, ok := d.Server(); !ok && len(d.Extra) == 0 && len(d.Hosts) == 0 {
		return ""
	}
	switch d.Strategy {
	case routing.OnlyIPv4:
		return "UseIPv4"
	case routing.BothIPs:
		return "UseIP"
	}
	return "UseIPv4v6"
}

func xrayDNS(d routing.DNSSettings) map[string]any {
	main, ok := d.Server()
	if !ok && len(d.Extra) == 0 && len(d.Hosts) == 0 {
		return nil // the system resolver, nothing to configure
	}
	addr := func(s routing.DNSServer) string {
		if s.URL != "" {
			return s.URL
		}
		return s.Host
	}
	var servers []any
	// Extra servers first: Xray tries a server whose domains match before
	// the plain ones.
	for _, e := range d.Extra {
		srv, err := routing.ResolveServer(e.Server)
		if err != nil {
			continue
		}
		servers = append(servers, map[string]any{"address": addr(srv), "domains": e.Domains, "skipFallback": true})
	}
	if ok {
		servers = append(servers, addr(main))
	} else {
		servers = append(servers, "localhost") // the system resolver
	}
	q := "UseIP"
	if d.Strategy == routing.OnlyIPv4 {
		q = "UseIPv4"
	}
	cfg := map[string]any{"servers": servers, "queryStrategy": q}
	for k, v := range map[string]bool{
		"disableCache": d.DisableCache, "disableFallback": d.DisableFallback,
		"disableFallbackIfMatch": d.DisableFallbackIfMatch, "enableParallelQuery": d.Parallel, "useSystemHosts": d.SystemHosts,
	} {
		if v {
			cfg[k] = true
		}
	}
	if d.ClientIP != "" {
		cfg["clientIp"] = d.ClientIP
	}
	if len(d.Hosts) > 0 {
		hosts := map[string]string{}
		for _, h := range d.Hosts {
			hosts[h.Name] = h.IP
		}
		cfg["hosts"] = hosts
	}
	return cfg
}

// xrayRules translates one rule. Inside an Xray rule all fields must match,
// so a rule with both domains and IPs becomes two rules (our match list is
// "any of").
func xrayRules(r routing.Final) (rules []any, usesIP bool) {
	domains, ips := split(r.Match)
	base := func() map[string]any {
		m := map[string]any{"outboundTag": outTag(r.Outbound)}
		if r.Ports != "" {
			m["port"] = r.Ports
		}
		if r.Network != "" {
			m["network"] = r.Network
		}
		if len(r.Protocols) > 0 {
			m["protocol"] = r.Protocols
		}
		if len(r.Users) > 0 {
			m["user"] = r.Users
		}
		return m
	}
	if len(domains) > 0 {
		m := base()
		m["domain"] = domains
		rules = append(rules, m)
	}
	if len(ips) > 0 {
		m := base()
		m["ip"] = ips
		rules = append(rules, m)
		usesIP = true
	}
	if len(r.Match) == 0 {
		rules = append(rules, base())
	}
	return rules, usesIP
}

func xrayInbound(ib store.Inbound, users []store.User) map[string]any {
	sniffing := map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true}
	if s := ib.Settings.Sniffing; s != nil {
		sniffing = map[string]any{
			"enabled": s.Enabled, "destOverride": s.DestOverride,
			"metadataOnly": s.MetadataOnly, "routeOnly": s.RouteOnly,
		}
	}
	in := map[string]any{
		"tag":      "in-" + strconv.FormatInt(ib.ID, 10),
		"listen":   "0.0.0.0",
		"port":     ib.Port,
		"sniffing": sniffing,
	}
	defer func() {
		// Socket options apply to every stream an inbound has.
		if so := ib.Settings.Sockopt; so != nil {
			stream, _ := in["streamSettings"].(map[string]any)
			if stream == nil {
				stream = map[string]any{}
				in["streamSettings"] = stream
			}
			stream["sockopt"] = sockopt(*so)
		}
	}()
	clients := []any{}
	switch ib.Protocol {
	case "vless":
		for _, u := range users {
			c := map[string]any{"id": u.UUID, "email": Email(u.ID)}
			if ib.Transport == "tcp" {
				c["flow"] = "xtls-rprx-vision"
			}
			clients = append(clients, c)
		}
		in["protocol"] = "vless"
		in["settings"] = map[string]any{"clients": clients, "decryption": "none"}
		in["streamSettings"] = realityStream(ib)
	case "trojan":
		for _, u := range users {
			clients = append(clients, map[string]any{"password": u.UUID, "email": Email(u.ID)})
		}
		in["protocol"] = "trojan"
		in["settings"] = map[string]any{"clients": clients}
		in["streamSettings"] = realityStream(ib)
	case "vmess":
		for _, u := range users {
			clients = append(clients, map[string]any{"id": u.UUID, "email": Email(u.ID)})
		}
		in["protocol"] = "vmess"
		in["settings"] = map[string]any{"clients": clients}
		stream := map[string]any{"network": "raw", "security": "none"}
		if ib.Transport == "ws" && ib.Settings.WS != nil {
			stream["network"] = "ws"
			stream["wsSettings"] = map[string]any{"path": ib.Settings.WS.Path}
		}
		in["streamSettings"] = stream
	case "shadowsocks":
		for _, u := range users {
			clients = append(clients, map[string]any{"password": SSUserKey(u.UUID), "email": Email(u.ID)})
		}
		ss := ib.Settings.Shadowsocks
		in["protocol"] = "shadowsocks"
		in["settings"] = map[string]any{
			"method": ss.Method, "password": ss.ServerPSK, "clients": clients, "network": "tcp,udp",
		}
	}
	return in
}

func sockopt(so store.SockoptSettings) map[string]any {
	m := map[string]any{}
	if so.TCPFastOpen {
		m["tcpFastOpen"] = true
	}
	if so.TCPCongestion != "" {
		m["tcpCongestion"] = so.TCPCongestion
	}
	if so.TCPMptcp {
		m["tcpMptcp"] = true
	}
	if so.AcceptProxyProtocol {
		m["acceptProxyProtocol"] = true
	}
	return m
}

// realityStream is the stream settings shared by VLESS and Trojan.
func realityStream(ib store.Inbound) map[string]any {
	r := ib.Settings.Reality
	rs := map[string]any{
		"show":        false,
		"target":      r.Target,
		"xver":        r.Xver,
		"serverNames": r.ServerNames,
		"privateKey":  r.PrivateKey,
		"shortIds":    r.ShortIDs,
	}
	if r.MaxTimeDiff > 0 {
		rs["maxTimeDiff"] = r.MaxTimeDiff
	}
	if r.MinClientVer != "" {
		rs["minClientVer"] = r.MinClientVer
	}
	if r.MaxClientVer != "" {
		rs["maxClientVer"] = r.MaxClientVer
	}
	if r.MLDSA65Seed != "" {
		rs["mldsa65Seed"] = r.MLDSA65Seed
	}
	stream := map[string]any{"security": "reality", "realitySettings": rs}
	switch ib.Transport {
	case "xhttp":
		stream["network"] = "xhttp"
		stream["xhttpSettings"] = XHTTPServer(ib.Settings.XHTTP)
	case "grpc":
		stream["network"] = "grpc"
		stream["grpcSettings"] = map[string]any{"serviceName": ib.Settings.GRPC.ServiceName}
	default:
		stream["network"] = "raw"
	}
	return stream
}

// warpMTU leaves room below WARP's nominal 1280: on some paths full-size
// packets vanish, which shows up as TLS handshakes that never finish (a
// modern ClientHello needs two segments).
const warpMTU = 1200

// warpEndpoint pins the peer to an IPv4 address: the hostname resolves to
// IPv6 first, which fails on servers without an IPv6 route.
func warpEndpoint(ep string) string {
	host, port, err := net.SplitHostPort(ep)
	if err != nil || net.ParseIP(host) == nil {
		return "162.159.192.1:2408"
	}
	return net.JoinHostPort(host, port)
}

// cascadeInbound — VLESS + Reality (TCP, Vision) с одним служебным
// клиентом. Его трафик не считается ни одному пользователю: он уже учтён
// на сервере, куда пользователь подключился.
func cascadeInbound(e routing.CustomOutbound) map[string]any {
	c := e.Cascade
	return map[string]any{
		"tag": "cascade_" + e.ID, "listen": "0.0.0.0", "port": c.Port, "protocol": "vless",
		"settings": map[string]any{
			"clients":    []any{map[string]any{"id": c.UUID, "flow": "xtls-rprx-vision", "email": "cascade-" + e.ID}},
			"decryption": "none",
		},
		"streamSettings": map[string]any{
			"network": "raw", "security": "reality",
			"realitySettings": map[string]any{
				"target": c.SNI + ":443", "serverNames": []string{c.SNI}, "privateKey": c.PrivateKey, "shortIds": []string{c.ShortID},
			},
		},
		"sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true},
	}
}
