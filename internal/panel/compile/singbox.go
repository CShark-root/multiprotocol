package compile

import (
	"encoding/json"
	"net"
	"strconv"

	"panel/internal/panel/store"
)

var singboxProtocols = map[string]bool{"tuic": true, "anytls": true, "shadowtls": true}

// SingBoxUserOutbound is the per-user outbound tag. sing-box's Clash API does
// not report which user opened a connection, but it does report the outbound
// chain, so every user gets their own (identical) direct outbound and the
// agent attributes traffic by it.
func SingBoxUserOutbound(email string) string { return "direct-" + email }

// SingBox builds the sing-box server config (without the node-local Clash
// API section, which the agent adds). It returns nil when there is nothing
// to serve.
func SingBox(inbounds []store.Inbound, users []store.User) ([]byte, error) {
	var ins []any
	for _, ib := range inbounds {
		if ib.Enabled && singboxProtocols[ib.Protocol] {
			ins = append(ins, singboxInbounds(ib, users)...)
		}
	}
	if len(ins) == 0 {
		return nil, nil
	}

	outbounds := []any{map[string]any{"type": "direct", "tag": "direct"}}
	rules := []any{
		map[string]any{"ip_cidr": privateCIDRs, "action": "reject"},
		map[string]any{"port": []int{25}, "network": []string{"tcp"}, "action": "reject"},
	}
	for _, u := range users {
		tag := SingBoxUserOutbound(Email(u.ID))
		outbounds = append(outbounds, map[string]any{"type": "direct", "tag": tag})
		rules = append(rules, map[string]any{"auth_user": []string{Email(u.ID)}, "action": "route", "outbound": tag})
	}
	cfg := map[string]any{
		"log":       map[string]any{"level": "warn", "timestamp": false},
		"inbounds":  ins,
		"outbounds": outbounds,
		"route":     map[string]any{"rules": rules, "final": "direct"},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func singboxInbounds(ib store.Inbound, users []store.User) []any {
	tag := "in-" + strconv.FormatInt(ib.ID, 10)
	base := func(typ string) map[string]any {
		return map[string]any{"type": typ, "tag": tag, "listen": "::", "listen_port": ib.Port}
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
