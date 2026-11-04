// Package outbound turns a share link (vless://, trojan://, ss://,
// vmess://, socks5://, http://) into an Xray outbound, so traffic can be
// sent on to another server or proxy.
package outbound

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Parsed is a link ready for Xray: protocol, settings and stream settings.
type Parsed struct {
	Protocol string         `json:"protocol"`
	Settings map[string]any `json:"settings"`
	Stream   map[string]any `json:"streamSettings,omitempty"`
	Host     string         `json:"host"` // for display
	Port     int            `json:"port"`
}

var ErrUnsupported = errors.New("unsupported link: vless://, trojan://, ss://, vmess://, socks5:// or http:// expected")

// Parse reads one share link.
func Parse(link string) (Parsed, error) {
	link = strings.TrimSpace(link)
	scheme, _, ok := strings.Cut(link, "://")
	if !ok {
		return Parsed{}, ErrUnsupported
	}
	switch strings.ToLower(scheme) {
	case "vless", "trojan":
		return parseVLESSTrojan(link)
	case "ss":
		return parseSS(link)
	case "vmess":
		return parseVMess(link)
	case "socks", "socks5", "http", "https":
		return parseProxy(link)
	}
	return Parsed{}, ErrUnsupported
}

func hostPort(u *url.URL) (string, int, error) {
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if host == "" || err != nil || port < 1 || port > 65535 {
		return "", 0, errors.New("the link has no valid host:port")
	}
	return host, port, nil
}

func parseVLESSTrojan(link string) (Parsed, error) {
	u, err := url.Parse(link)
	if err != nil {
		return Parsed{}, err
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Parsed{}, err
	}
	id := u.User.Username()
	if id == "" {
		return Parsed{}, errors.New("the link has no id or password")
	}
	q := u.Query()
	p := Parsed{Protocol: strings.ToLower(u.Scheme), Host: host, Port: port}
	if p.Protocol == "vless" {
		user := map[string]any{"id": id, "encryption": "none"}
		if f := q.Get("flow"); f != "" {
			user["flow"] = f
		}
		p.Settings = map[string]any{"vnext": []any{map[string]any{"address": host, "port": port, "users": []any{user}}}}
	} else {
		p.Settings = map[string]any{"servers": []any{map[string]any{"address": host, "port": port, "password": id}}}
	}
	p.Stream = stream(q, host)
	return p, nil
}

// stream builds streamSettings from the common query parameters.
func stream(q url.Values, host string) map[string]any {
	network := q.Get("type")
	if network == "" || network == "tcp" {
		network = "raw"
	}
	s := map[string]any{"network": network}
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("peer")
	}
	switch q.Get("security") {
	case "reality":
		s["security"] = "reality"
		s["realitySettings"] = map[string]any{
			"serverName": sni, "fingerprint": or(q.Get("fp"), "chrome"),
			"publicKey": q.Get("pbk"), "shortId": q.Get("sid"), "spiderX": q.Get("spx"),
		}
	case "tls":
		t := map[string]any{"serverName": or(sni, host), "fingerprint": or(q.Get("fp"), "chrome")}
		if a := q.Get("alpn"); a != "" {
			t["alpn"] = strings.Split(a, ",")
		}
		if q.Get("allowInsecure") == "1" || q.Get("insecure") == "1" {
			t["allowInsecure"] = true
		}
		s["security"], s["tlsSettings"] = "tls", t
	}
	switch network {
	case "ws":
		s["wsSettings"] = map[string]any{"path": or(q.Get("path"), "/"), "host": q.Get("host")}
	case "grpc":
		s["grpcSettings"] = map[string]any{"serviceName": q.Get("serviceName")}
	case "xhttp":
		x := map[string]any{"path": or(q.Get("path"), "/"), "mode": or(q.Get("mode"), "auto")}
		if h := q.Get("host"); h != "" {
			x["host"] = h
		}
		s["xhttpSettings"] = x
	case "httpupgrade":
		s["httpupgradeSettings"] = map[string]any{"path": or(q.Get("path"), "/"), "host": q.Get("host")}
	}
	return s
}

func or(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// parseSS accepts SIP002 (ss://base64(method:password)@host:port) and the
// older ss://base64(method:password@host:port).
func parseSS(link string) (Parsed, error) {
	rest := strings.TrimPrefix(link, link[:strings.Index(link, "://")+3])
	rest, _, _ = strings.Cut(rest, "#")
	rest, _, _ = strings.Cut(rest, "?")
	var method, password, hostport string
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		info, err := unb64(rest[:at])
		if err != nil {
			info, _ = url.PathUnescape(rest[:at]) // SIP022: method:password in clear
		}
		method, password, _ = strings.Cut(info, ":")
		hostport = rest[at+1:]
	} else {
		dec, err := unb64(rest)
		if err != nil {
			return Parsed{}, errors.New("unreadable ss:// link")
		}
		at := strings.LastIndex(dec, "@")
		if at < 0 {
			return Parsed{}, errors.New("unreadable ss:// link")
		}
		method, password, _ = strings.Cut(dec[:at], ":")
		hostport = dec[at+1:]
	}
	host, portStr, err := net.SplitHostPort(strings.TrimSuffix(hostport, "/"))
	port, _ := strconv.Atoi(portStr)
	if err != nil || method == "" || password == "" || port == 0 {
		return Parsed{}, errors.New("unreadable ss:// link")
	}
	return Parsed{Protocol: "shadowsocks", Host: host, Port: port, Settings: map[string]any{
		"servers": []any{map[string]any{"address": host, "port": port, "method": method, "password": password}},
	}}, nil
}

func unb64(s string) (string, error) {
	s, _ = url.PathUnescape(s)
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return string(b), nil
		}
	}
	return "", errors.New("not base64")
}

func parseVMess(link string) (Parsed, error) {
	raw, err := unb64(strings.TrimPrefix(link, link[:strings.Index(link, "://")+3]))
	if err != nil {
		return Parsed{}, errors.New("unreadable vmess:// link")
	}
	var v struct {
		Add  string          `json:"add"`
		Port json.RawMessage `json:"port"`
		ID   string          `json:"id"`
		Net  string          `json:"net"`
		Host string          `json:"host"`
		Path string          `json:"path"`
		TLS  string          `json:"tls"`
		SNI  string          `json:"sni"`
		Scy  string          `json:"scy"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return Parsed{}, errors.New("unreadable vmess:// link")
	}
	port, _ := strconv.Atoi(strings.Trim(string(v.Port), `"`))
	if v.Add == "" || v.ID == "" || port == 0 {
		return Parsed{}, errors.New("the vmess link misses address, port or id")
	}
	q := url.Values{"type": {v.Net}, "host": {v.Host}, "path": {v.Path}, "sni": {v.SNI}}
	if v.TLS == "tls" {
		q.Set("security", "tls")
	}
	return Parsed{Protocol: "vmess", Host: v.Add, Port: port, Stream: stream(q, v.Add), Settings: map[string]any{
		"vnext": []any{map[string]any{"address": v.Add, "port": port, "users": []any{map[string]any{"id": v.ID, "security": or(v.Scy, "auto")}}}},
	}}, nil
}

func parseProxy(link string) (Parsed, error) {
	u, err := url.Parse(link)
	if err != nil {
		return Parsed{}, err
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Parsed{}, err
	}
	server := map[string]any{"address": host, "port": port}
	if u.User != nil {
		pass, _ := u.User.Password()
		server["users"] = []any{map[string]any{"user": u.User.Username(), "pass": pass}}
	}
	p := Parsed{Host: host, Port: port, Settings: map[string]any{"servers": []any{server}}}
	switch strings.ToLower(u.Scheme) {
	case "socks", "socks5":
		p.Protocol = "socks"
	case "http":
		p.Protocol = "http"
	case "https":
		p.Protocol = "http"
		p.Stream = map[string]any{"security": "tls", "tlsSettings": map[string]any{"serverName": host}}
	}
	return p, nil
}

// Describe is a short human label: "VLESS · example.com:443".
func (p Parsed) Describe() string {
	names := map[string]string{"vless": "VLESS", "trojan": "Trojan", "shadowsocks": "Shadowsocks", "vmess": "VMess", "socks": "SOCKS5", "http": "HTTP"}
	return fmt.Sprintf("%s · %s", names[p.Protocol], net.JoinHostPort(p.Host, strconv.Itoa(p.Port)))
}
