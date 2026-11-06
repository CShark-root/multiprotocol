package compile

import (
	"encoding/json"
	"net"
	"slices"
	"strconv"
	"strings"

	"panel/internal/panel/geo"
	"panel/internal/panel/routing"
	"panel/internal/panel/store"
)

var singboxProtocols = map[string]bool{"tuic": true, "anytls": true, "shadowtls": true, "vless-sb": true}

// sing-box's Clash API does not report which user opened a connection,
// but it does report the outbound chain, so every user gets their own
// (otherwise identical) outbounds and the agent attributes traffic by them.

// SingBox builds the sing-box server config (without the node-local Clash
// API section, which the agent adds). It returns nil when there is nothing
// to serve.
func SingBox(inbounds []store.Inbound, users []store.User, rt Routing) ([]byte, error) {
	var ins []any
	for _, ib := range inbounds {
		if ib.Enabled && singboxProtocols[ib.Protocol] {
			ins = append(ins, singboxInbounds(ib, users)...)
		}
	}
	if len(ins) == 0 {
		return nil, nil
	}
	// WARP goes through Xray's loopback bridge, like Hysteria: one WARP
	// device for all cores, and per-user outbounds keep traffic accounting.
	warp := rt.usesWarp() && rt.Bridge.Port > 0
	dnsCfg, strategy := singboxDNSConfig(rt)

	// Every user gets their own outbound of each kind, so the agent can
	// attribute traffic by the outbound tag whatever the rules choose.
	kinds := []string{routing.Direct, routing.IPv4}
	if warp {
		kinds = append(kinds, routing.WARP)
	}
	for _, c := range rt.Custom {
		if c.Bridge.Port > 0 {
			kinds = append(kinds, c.Tag)
		}
	}
	outbounds := []any{map[string]any{"type": "direct", "tag": "direct"}}
	for _, u := range users {
		for _, k := range kinds {
			o := map[string]any{"type": "direct", "tag": singboxUserTag(k, Email(u.ID))}
			switch k {
			case routing.IPv4:
				o["domain_resolver"] = map[string]any{"server": "dns", "strategy": "ipv4_only"}
			case routing.WARP:
				o["type"] = "socks"
				o["server"], o["server_port"] = "127.0.0.1", rt.Bridge.Port
				o["username"], o["password"] = rt.Bridge.User, rt.Bridge.Password
			default:
				if c := rt.custom(k); c != nil {
					o["type"] = "socks"
					o["server"], o["server_port"] = "127.0.0.1", c.Bridge.Port
					o["username"], o["password"] = c.Bridge.User, c.Bridge.Password
				}
			}
			outbounds = append(outbounds, o)
		}
	}

	// Sniffing first: domain and protocol rules need it.
	rules := []any{map[string]any{"action": "sniff"}}
	var ruleSets []any
	for i, r := range rt.Rules {
		cond, ok := singboxMatch(r, rt)
		if !ok {
			continue // its categories are missing from the database: never widen the rule
		}
		if cond != nil {
			tag := "rs-" + strconv.Itoa(i)
			ruleSets = append(ruleSets, map[string]any{"type": "inline", "tag": tag, "rules": []any{cond}})
		}
		base := func() map[string]any {
			m := map[string]any{}
			if cond != nil {
				m["rule_set"] = []string{"rs-" + strconv.Itoa(i)}
			}
			if len(r.Protocols) > 0 {
				m["protocol"] = r.Protocols
			}
			return m
		}
		if r.Outbound == routing.Block {
			m := base()
			if len(r.Users) > 0 {
				m["auth_user"] = r.Users
			}
			m["action"] = "reject"
			rules = append(rules, m)
			continue
		}
		kind := r.Outbound
		if kind == routing.WARP && !warp {
			kind = routing.Direct
		}
		if strings.HasPrefix(kind, routing.OutPrefix) {
			if c := rt.custom(outTag(kind)); c != nil && c.Bridge.Port > 0 {
				kind = c.Tag
			} else {
				kind = routing.Direct
			}
		}
		for _, u := range users {
			email := Email(u.ID)
			if len(r.Users) > 0 && !slices.Contains(r.Users, email) {
				continue
			}
			m := base()
			m["auth_user"] = []string{email}
			m["action"] = "route"
			m["outbound"] = singboxUserTag(kind, email)
			rules = append(rules, m)
		}
	}
	final := rt.Final
	if final == routing.WARP && !warp {
		final = routing.Direct
	}
	for _, u := range users {
		rules = append(rules, map[string]any{"auth_user": []string{Email(u.ID)}, "action": "route", "outbound": singboxUserTag(final, Email(u.ID))})
	}

	route := map[string]any{"rules": rules, "final": "direct", "default_domain_resolver": map[string]any{"server": "dns"}}
	if strategy != "" {
		route["default_domain_resolver"] = map[string]any{"server": "dns", "strategy": strategy}
	}
	if len(ruleSets) > 0 {
		route["rule_set"] = ruleSets
	}
	cfg := map[string]any{
		"log":       map[string]any{"level": "warn", "timestamp": false},
		"dns":       dnsCfg,
		"inbounds":  ins,
		"outbounds": outbounds,
		"route":     route,
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// singboxUserTag is "<kind>-<email>", e.g. "warp-u7"; the agent reads the
// user back from the part after the dash.
func singboxUserTag(kind, email string) string { return kind + "-" + email }

func singboxDNS(d routing.DNSSettings) (server map[string]any, strategy string) {
	switch d.Strategy {
	case routing.OnlyIPv4:
		strategy = "ipv4_only"
	case routing.PreferIPv4:
		strategy = "prefer_ipv4"
	}
	srv, ok := d.Server()
	switch {
	case !ok:
		return map[string]any{"type": "local", "tag": "dns"}, strategy
	case srv.URL != "":
		host, path := srv.Host, "/dns-query"
		if _, rest, found := strings.Cut(strings.TrimPrefix(srv.URL, "https://"), "/"); found {
			path = "/" + rest
		}
		m := map[string]any{"type": "https", "tag": "dns", "server": host, "path": path}
		if h, p, err := net.SplitHostPort(host); err == nil {
			port, _ := strconv.Atoi(p)
			m["server"], m["server_port"] = strings.Trim(h, "[]"), port
		}
		return m, strategy
	}
	return map[string]any{"type": "udp", "tag": "dns", "server": srv.Host}, strategy
}

// singboxMatch builds the headless rule for a rule's match list, ports and
// network, with geo categories resolved from the databases. It returns
// ok=false when the rule names categories none of which could be resolved,
// and nil when the rule has no such conditions at all.
func singboxMatch(r routing.Final, rt Routing) (map[string]any, bool) {
	var suffix, full, keyword, regex, cidr []string
	resolved := len(r.Match) == 0
	for _, m := range r.Match {
		kind, val, _ := strings.Cut(m, ":")
		switch kind {
		case "domain":
			suffix = append(suffix, val)
		case "full":
			full = append(full, val)
		case "keyword":
			keyword = append(keyword, val)
		case "regexp":
			regex = append(regex, val)
		case "geosite":
			name, attr, _ := strings.Cut(val, "@")
			site, ok := rt.Sites[name]
			if !ok {
				continue
			}
			for _, d := range site.Domains {
				if attr != "" && !slices.Contains(d.Attrs, attr) {
					continue
				}
				switch d.Kind {
				case geo.KindSuffix:
					suffix = append(suffix, d.Value)
				case geo.KindFull:
					full = append(full, d.Value)
				case geo.KindKeyword:
					keyword = append(keyword, d.Value)
				case geo.KindRegex:
					regex = append(regex, d.Value)
				}
			}
		case "geoip":
			if val == "private" {
				cidr = append(cidr, privateCIDRs...)
				break
			}
			for _, p := range rt.IPs[val] {
				cidr = append(cidr, p.String())
			}
		default:
			cidr = append(cidr, m)
		}
	}
	m := map[string]any{}
	for k, v := range map[string][]string{"domain_suffix": suffix, "domain": full, "domain_keyword": keyword, "domain_regex": regex, "ip_cidr": cidr} {
		if len(v) > 0 {
			m[k] = v
			resolved = true
		}
	}
	if !resolved {
		return nil, false
	}
	var ports []int
	var ranges []string
	for _, pr := range portRanges(r.Ports) {
		if pr[0] == pr[1] {
			ports = append(ports, pr[0])
		} else {
			ranges = append(ranges, strconv.Itoa(pr[0])+":"+strconv.Itoa(pr[1]))
		}
	}
	if len(ports) > 0 {
		m["port"] = ports
	}
	if len(ranges) > 0 {
		m["port_range"] = ranges
	}
	if r.Network != "" {
		m["network"] = []string{r.Network}
	}
	if len(m) == 0 {
		return nil, true
	}
	return m, true
}

func singboxInbounds(ib store.Inbound, users []store.User) []any {
	tag := "in-" + strconv.FormatInt(ib.ID, 10)
	listen := "::"
	if ib.LocalOnly {
		listen = "127.0.0.1"
	}
	base := func(typ string) map[string]any {
		return map[string]any{"type": typ, "tag": tag, "listen": listen, "listen_port": ib.Port}
	}
	tls := func(alpn []string) map[string]any {
		t := ib.Settings.TLS
		m := map[string]any{
			"enabled": true, "server_name": t.SNI,
			"certificate": t.CertPEM, "key": t.KeyPEM,
		}
		if alpn != nil {
			m["alpn"] = alpn
		}
		return m
	}

	switch ib.Protocol {
	case "vless-sb":
		// VLESS + Reality (TCP, Vision) на ядре sing-box. Проверено на VM:
		// пускает клиентов sing-box, Xray и Mihomo (с ML-KEM и без).
		r := ib.Settings.Reality
		host, portStr, _ := net.SplitHostPort(r.Target)
		port, _ := strconv.Atoi(portStr)
		in := base("vless")
		list := []any{}
		for _, u := range users {
			list = append(list, map[string]any{"name": Email(u.ID), "uuid": u.UUID, "flow": "xtls-rprx-vision"})
		}
		in["users"] = list
		in["tls"] = map[string]any{
			"enabled": true, "server_name": r.ServerNames[0],
			"reality": map[string]any{
				"enabled":     true,
				"handshake":   map[string]any{"server": host, "server_port": port},
				"private_key": r.PrivateKey, "short_id": r.ShortIDs,
			},
		}
		return []any{in}
	case "tuic":
		in := base("tuic")
		list := []any{}
		for _, u := range users {
			list = append(list, map[string]any{"name": Email(u.ID), "uuid": u.UUID, "password": u.UUID})
		}
		in["users"] = list
		in["congestion_control"] = "bbr"
		in["tls"] = tls([]string{"h3"})
		return []any{in}
	case "anytls":
		in := base("anytls")
		list := []any{}
		for _, u := range users {
			list = append(list, map[string]any{"name": Email(u.ID), "password": u.UUID})
		}
		in["users"] = list
		in["tls"] = tls(nil)
		return []any{in}
	case "shadowtls":
		// ShadowTLS v3 borrows the TLS handshake of a real site and hands the
		// authenticated stream to an inner Shadowsocks-2022 inbound.
		host, portStr, _ := net.SplitHostPort(ib.Settings.ShadowTLS.Handshake)
		port, _ := strconv.Atoi(portStr)
		inner := tag + "-ss"
		outer := base("shadowtls")
		stUsers, ssUsers := []any{}, []any{}
		for _, u := range users {
			stUsers = append(stUsers, map[string]any{"name": Email(u.ID), "password": u.UUID})
			ssUsers = append(ssUsers, map[string]any{"name": Email(u.ID), "password": SSUserKey(u.UUID)})
		}
		outer["version"] = 3
		outer["users"] = stUsers
		outer["handshake"] = map[string]any{"server": host, "server_port": port}
		outer["strict_mode"] = true
		outer["detour"] = inner
		ss := ib.Settings.Shadowsocks
		return []any{outer, map[string]any{
			"type": "shadowsocks", "tag": inner, "listen": "127.0.0.1",
			"method": ss.Method, "password": ss.ServerPSK, "users": ssUsers,
		}}
	}
	return nil
}

// singboxDNSConfig is the main server (tag "dns"), the extra per-site
// servers with their rules, fixed hosts and the cache flag.
func singboxDNSConfig(rt Routing) (map[string]any, string) {
	main, strategy := singboxDNS(rt.DNS)
	servers := []any{main}
	var rules []any
	if len(rt.DNS.Hosts) > 0 {
		pre := map[string]any{}
		var names []string
		for _, h := range rt.DNS.Hosts {
			pre[h.Name] = []string{h.IP}
			names = append(names, h.Name)
		}
		servers = append(servers, map[string]any{"type": "hosts", "tag": "hosts", "predefined": pre})
		rules = append(rules, map[string]any{"domain": names, "server": "hosts"})
	}
	for i, e := range rt.DNS.Extra {
		srv, err := routing.ResolveServer(e.Server)
		if err != nil {
			continue
		}
		tag := "dns-" + strconv.Itoa(i+1)
		m, _ := singboxDNS(routing.DNSSettings{Upstream: "custom", Custom: firstNonEmpty(srv.URL, srv.Host)})
		m["tag"] = tag
		cond, ok := singboxMatch(routing.Final{Match: e.Domains}, rt)
		if !ok || cond == nil {
			continue
		}
		cond["server"] = tag
		servers = append(servers, m)
		rules = append(rules, cond)
	}
	cfg := map[string]any{"servers": servers}
	if len(rules) > 0 {
		cfg["rules"] = rules
	}
	if rt.DNS.DisableCache {
		cfg["disable_cache"] = true
	}
	if rt.DNS.ClientIP != "" {
		cfg["client_subnet"] = rt.DNS.ClientIP
	}
	return cfg, strategy
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
