package subscription

import (
	"encoding/json"
	"net"
	"strconv"

	"panel/internal/panel/compile"
	"panel/internal/panel/routing"
	"panel/internal/panel/store"
)

// Подписка для клиентов на Mihomo (FlClash, Clash Meta for Android, Clash
// Verge Rev). Отдаётся как JSON: это тоже YAML, и Mihomo читает его как есть.
// Группа «Авто» сама выбирает работающее подключение, «VPN» — ручной выбор.

// geodata — гео-базы runetfreedom для правил «российское напрямую» и
// «только заблокированное».
const geodataBase = "https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/"

const (
	groupProxy = "VPN"
	groupAuto  = "Авто"
)

func MihomoConfig(inbounds []store.Inbound, u store.User, host, client string) ([]byte, error) {
	var proxies []any
	var names []string
	seen := map[string]int{}
	for _, ib := range inbounds {
		if !ib.Enabled {
			continue
		}
		p := mihomoProxy(ib, u, host)
		if p == nil {
			continue
		}
		// Имена в Mihomo должны быть уникальными.
		name := p["name"].(string)
		if n := seen[name]; n > 0 {
			name += " " + strconv.Itoa(n+1)
			p["name"] = name
		}
		seen[p["name"].(string)]++
		proxies = append(proxies, p)
		names = append(names, name)
	}
	groups := []any{
		map[string]any{"name": groupProxy, "type": "select", "proxies": append([]string{groupAuto}, names...)},
		map[string]any{"name": groupAuto, "type": "url-test", "proxies": names, "url": "https://www.gstatic.com/generate_204", "interval": 180, "tolerance": 100},
	}
	if len(names) == 0 {
		groups = []any{map[string]any{"name": groupProxy, "type": "select", "proxies": []string{"DIRECT"}}}
	}
	cfg := map[string]any{
		"mixed-port":     7890,
		"allow-lan":      false,
		"mode":           "rule",
		"log-level":      "warning",
		"ipv6":           true,
		"unified-delay":  true,
		"tcp-concurrent": true,
		"geodata-mode":   true,
		"geox-url": map[string]any{
			"geoip":   geodataBase + "geoip.dat",
			"geosite": geodataBase + "geosite.dat",
		},
		"geo-auto-update":     true,
		"geo-update-interval": 24,
		// DNS не задаём: приложение (FlClash и др.) пользуется своим. Наши 1.1.1.1
		// и 8.8.8.8 у российских операторов режут — тогда не узнать адрес сервера.
		"proxies":      proxies,
		"proxy-groups": groups,
		"rules":        mihomoRules(client),
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// mihomoRules — разделение трафика на устройстве, как для sing-box.
func mihomoRules(mode string) []string {
	base := []string{"GEOIP,private,DIRECT,no-resolve"}
	switch mode {
	case routing.ClientRuDirect:
		return append(base,
			"DOMAIN-SUFFIX,ru,DIRECT", "DOMAIN-SUFFIX,xn--p1ai,DIRECT", "DOMAIN-SUFFIX,su,DIRECT",
			"GEOSITE,category-ru,DIRECT", "GEOIP,ru,DIRECT", "MATCH,"+groupProxy)
	case routing.ClientBlockedOnly:
		return append(base, "GEOSITE,ru-blocked,"+groupProxy, "GEOIP,ru-blocked,"+groupProxy, "MATCH,DIRECT")
	}
	return append(base, "MATCH,"+groupProxy)
}

func mihomoProxy(ib store.Inbound, u store.User, host string) map[string]any {
	h, portStr, _ := net.SplitHostPort(endpoint(ib, host))
	port, _ := strconv.Atoi(portStr)
	p := map[string]any{"name": ib.Remark, "server": h, "port": port, "udp": true}
	s := ib.Settings
	// Сертификат: доверенный домена — проверяется как у сайта, самоподписанный — по отпечатку.
	cert := func(sni, pin string, trusted bool) {
		p["sni"] = sni
		if !trusted && pin != "" {
			p["fingerprint"] = pin
			p["skip-cert-verify"] = true
		}
	}
	reality := func() {
		r := s.Reality
		p["tls"] = true
		p["servername"] = r.ServerNames[0]
		p["client-fingerprint"] = r.Fingerprint
		p["reality-opts"] = map[string]any{"public-key": r.PublicKey, "short-id": r.ShortIDs[0]}
	}
	switch ib.Protocol {
	case "vless", "trojan":
		// Поля — как у самого Mihomo, когда он разбирает нашу ссылку vless://
		// (с ней FlClash работает на VPS).
		if s.Reality == nil {
			return nil
		}
		p["type"] = ib.Protocol
		if ib.Protocol == "vless" {
			p["uuid"] = u.UUID
			p["encryption"], p["xudp"] = "none", true
		} else {
			p["password"] = u.UUID
		}
		reality()
		switch ib.Transport {
		case "tcp":
			p["network"] = "tcp"
			if ib.Protocol == "vless" {
				p["flow"] = "xtls-rprx-vision"
			}
		case "grpc":
			p["network"] = "grpc"
			p["grpc-opts"] = map[string]any{"grpc-service-name": s.GRPC.ServiceName}
		case "xhttp":
			x := s.XHTTP
			p["network"] = "xhttp"
			opts := map[string]any{"path": x.Path}
			if x.Host != "" {
				opts["host"] = x.Host
			}
			opts["mode"] = "auto"
			if x.Mode != "" {
				opts["mode"] = x.Mode
			}
			mihomoXHTTP(opts, x)
			p["xhttp-opts"] = opts
		default:
			return nil
		}
	case "vmess":
		p["type"], p["uuid"], p["alterId"], p["cipher"] = "vmess", u.UUID, 0, "auto"
		if ib.Transport == "ws" && s.WS != nil {
			p["network"] = "ws"
			p["ws-opts"] = map[string]any{"path": s.WS.Path}
		}
	case "shadowsocks":
		p["type"], p["cipher"] = "ss", s.Shadowsocks.Method
		p["password"] = s.Shadowsocks.ServerPSK + ":" + compile.SSUserKey(u.UUID)
	case "shadowtls":
		hsHost, _, _ := net.SplitHostPort(s.ShadowTLS.Handshake)
		p["type"], p["cipher"] = "ss", s.Shadowsocks.Method
		p["password"] = s.Shadowsocks.ServerPSK + ":" + compile.SSUserKey(u.UUID)
		p["client-fingerprint"] = "chrome"
		p["plugin"] = "shadow-tls"
		p["plugin-opts"] = map[string]any{"host": hsHost, "password": u.UUID, "version": 3}
	case "hysteria2":
		hy := s.Hysteria
		p["type"], p["password"] = "hysteria2", u.UUID
		cert(hy.SNI, hy.PinSHA256, hy.DomainCert)
		if hy.ObfsPassword != "" {
			p["obfs"], p["obfs-password"] = "salamander", hy.ObfsPassword
		}
		if hy.HopPorts != "" {
			p["ports"] = hy.HopPorts
			if hy.HopInterval > 0 {
				p["hop-interval"] = hy.HopInterval
			}
		}
	case "tuic":
		p["type"], p["uuid"], p["password"] = "tuic", u.UUID, u.UUID
		p["alpn"] = []string{"h3"}
		p["congestion-controller"], p["udp-relay-mode"] = "bbr", "native"
		cert(s.TLS.SNI, s.TLS.PinSHA256, s.TLS.DomainCert)
	case "anytls":
		p["type"], p["password"] = "anytls", u.UUID
		p["client-fingerprint"] = "chrome"
		cert(s.TLS.SNI, s.TLS.PinSHA256, s.TLS.DomainCert)
	default:
		return nil
	}
	return p
}

// mihomoXHTTP переносит настройки XHTTP, которые должен повторять клиент,
// под именами Mihomo (как в «extra» у ссылки). Незнакомые ключи Mihomo
// пропускает.
func mihomoXHTTP(opts map[string]any, x *store.XHTTPSettings) {
	set := func(k, v string) {
		if v != "" {
			opts[k] = v
		}
	}
	if len(x.Headers) > 0 {
		opts["headers"] = x.Headers
	}
	// x-padding-bytes не передаём: Mihomo понимает его иначе, чем Xray, и с ним
	// подключение падает (проверено в FlClash на VPS). Без него — работает.
	set("x-padding-method", x.PaddingMethod)
	set("x-padding-placement", x.PaddingPlacement)
	if x.PaddingObfsMode {
		opts["x-padding-obfs-mode"] = true
	}
	set("uplink-http-method", x.UplinkHTTPMethod)
	set("session-placement", x.SessionPlacement)
	set("seq-placement", x.SeqPlacement)
	set("uplink-data-placement", x.UplinkDataPlacement)
	if x.NoGRPCHeader {
		opts["no-grpc-header"] = true
	}
	set("sc-max-each-post-bytes", x.MaxEachPostBytes)
	set("sc-min-posts-interval-ms", x.MinPostsIntervalMs)
	if m := x.Xmux; m != nil {
		r := map[string]any{}
		for k, v := range map[string]string{
			"max-concurrency": m.MaxConcurrency, "max-connections": m.MaxConnections, "c-max-reuse-times": m.CMaxReuseTimes,
			"h-max-request-times": m.HMaxRequestTimes, "h-max-reusable-secs": m.HMaxReusableSecs,
		} {
			if v != "" {
				r[k] = v
			}
		}
		if m.HKeepAlivePeriod > 0 {
			r["h-keep-alive-period"] = m.HKeepAlivePeriod
		}
		if len(r) > 0 {
			opts["reuse-settings"] = r
		}
	}
}
