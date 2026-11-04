package subscription

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"

	"panel/internal/panel/compile"
	"panel/internal/panel/store"
)

// SingBoxConfig returns a complete sing-box client config (TUN mode) with
// one outbound per enabled inbound and an "auto" url-test group, so the
// client switches to whatever protocol currently works.
//
// Skipped: XHTTP (sing-box does not implement it) and, for now, all Reality
// inbounds. Tested on the VM, sing-box 1.14.4 as a client fails against the
// Xray 26.10.31 Reality server with "reality verification failed" for every
// uTLS fingerprint, while Xray clients connect fine. Reality users get the
// share links / base64 subscription for Xray-based apps instead. Re-check
// with newer versions and drop this when they interoperate again.
const singboxRealityWorks = false

func SingBoxConfig(inbounds []store.Inbound, u store.User, host string) ([]byte, error) {
	var outbounds []any
	var tags []string
	for _, ib := range inbounds {
		if !ib.Enabled {
			continue
		}
		obs := singboxOutbounds(ib, u, host)
		if len(obs) == 0 {
			continue
		}
		tags = append(tags, obs[0]["tag"].(string))
		for _, o := range obs {
			outbounds = append(outbounds, o)
		}
	}

	groups := []any{
		map[string]any{"type": "selector", "tag": "proxy", "outbounds": append([]string{"auto"}, tags...), "default": "auto"},
		map[string]any{"type": "urltest", "tag": "auto", "outbounds": tags, "interval": "3m", "tolerance": 100},
	}
	if len(tags) == 0 {
		groups = []any{map[string]any{"type": "selector", "tag": "proxy", "outbounds": []string{"direct"}}}
	}
	cfg := map[string]any{
		"log": map[string]any{"level": "warn"},
		"dns": map[string]any{
			"servers": []any{
				map[string]any{"type": "https", "tag": "dns-remote", "server": "1.1.1.1", "detour": "proxy"},
				map[string]any{"type": "local", "tag": "dns-local"},
			},
			"final": "dns-remote",
		},
		"inbounds": []any{map[string]any{
			"type": "tun", "tag": "tun-in", "address": []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
			"auto_route": true, "strict_route": true, "stack": "mixed",
		}},
		"outbounds": append(append(groups, outbounds...), map[string]any{"type": "direct", "tag": "direct"}),
		"route": map[string]any{
			"auto_detect_interface":   true,
			"default_domain_resolver": "dns-local",
			"final":                   "proxy",
			"rules": []any{
				map[string]any{"action": "sniff"},
				map[string]any{"protocol": "dns", "action": "hijack-dns"},
				map[string]any{"ip_is_private": true, "outbound": "direct"},
			},
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func singboxOutbounds(ib store.Inbound, u store.User, host string) []map[string]any {
	h, portStr, _ := net.SplitHostPort(endpoint(ib, host))
	port, _ := strconv.Atoi(portStr)
	tag := ib.Remark
	o := map[string]any{"tag": tag, "server": h, "server_port": port}
	s := ib.Settings

	reality := func() map[string]any {
		r := s.Reality
		t := map[string]any{
			"enabled": true, "server_name": r.ServerNames[0],
			"utls":    map[string]any{"enabled": true, "fingerprint": r.Fingerprint},
			"reality": map[string]any{"enabled": true, "public_key": r.PublicKey, "short_id": r.ShortIDs[0]},
		}
		return t
	}
	// Self-signed certificates are accepted as is; the domain certificate is
	// verified like any website's.
	insecureTLS := func(sni string, alpn []string, trusted bool) map[string]any {
		t := map[string]any{"enabled": true, "server_name": sni, "insecure": !trusted}
		if alpn != nil {
			t["alpn"] = alpn
		}
		return t
	}

	switch ib.Protocol {
	case "vless", "trojan":
		if !singboxRealityWorks || ib.Transport == "xhttp" || s.Reality == nil {
			return nil
		}
		o["type"] = ib.Protocol
		if ib.Protocol == "vless" {
			o["uuid"] = u.UUID
			if ib.Transport == "tcp" {
				o["flow"] = "xtls-rprx-vision"
			}
		} else {
			o["password"] = u.UUID
		}
		o["tls"] = reality()
		if ib.Transport == "grpc" {
			o["transport"] = map[string]any{"type": "grpc", "service_name": s.GRPC.ServiceName}
		}
	case "vmess":
		o["type"], o["uuid"], o["security"], o["alter_id"] = "vmess", u.UUID, "auto", 0
		if ib.Transport == "ws" && s.WS != nil {
			o["transport"] = map[string]any{"type": "ws", "path": s.WS.Path}
		}
	case "shadowsocks":
		o["type"], o["method"] = "shadowsocks", s.Shadowsocks.Method
		o["password"] = s.Shadowsocks.ServerPSK + ":" + compile.SSUserKey(u.UUID)
	case "hysteria2":
		o["type"], o["password"] = "hysteria2", u.UUID
		o["tls"] = insecureTLS(s.Hysteria.SNI, nil, s.Hysteria.DomainCert)
		if s.Hysteria.ObfsPassword != "" {
			o["obfs"] = map[string]any{"type": "salamander", "password": s.Hysteria.ObfsPassword}
		}
		if s.Hysteria.HopPorts != "" {
			o["server_ports"] = []string{strings.Replace(s.Hysteria.HopPorts, "-", ":", 1)}
			o["hop_interval"] = strconv.Itoa(s.Hysteria.HopInterval) + "s"
		}
	case "tuic":
		o["type"], o["uuid"], o["password"] = "tuic", u.UUID, u.UUID
		o["congestion_control"], o["udp_relay_mode"] = "bbr", "native"
		o["tls"] = insecureTLS(s.TLS.SNI, []string{"h3"}, s.TLS.DomainCert)
	case "anytls":
		o["type"], o["password"] = "anytls", u.UUID
		o["tls"] = insecureTLS(s.TLS.SNI, nil, s.TLS.DomainCert)
	case "shadowtls":
		hsHost, _, _ := net.SplitHostPort(s.ShadowTLS.Handshake)
		stls := map[string]any{
			"type": "shadowtls", "tag": tag + " (shadowtls)", "server": h, "server_port": port,
			"version": 3, "password": u.UUID,
			"tls": map[string]any{
				"enabled": true, "server_name": hsHost,
				"utls": map[string]any{"enabled": true, "fingerprint": "chrome"},
			},
		}
		inner := map[string]any{
			"type": "shadowsocks", "tag": tag, "method": s.Shadowsocks.Method,
			"password": s.Shadowsocks.ServerPSK + ":" + compile.SSUserKey(u.UUID),
			"detour":   stls["tag"],
		}
		return []map[string]any{inner, stls}
	default:
		return nil
	}
	return []map[string]any{o}
}
