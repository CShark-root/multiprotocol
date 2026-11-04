package subscription

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"panel/internal/panel/store"
)

var user = store.User{ID: 7, Name: "phone", UUID: "0123abcd-4567-4890-abcd-ef0123456789"}

func reality() *store.RealitySettings {
	return &store.RealitySettings{ServerNames: []string{"www.microsoft.com"}, PublicKey: "PUB", ShortIDs: []string{"ab"}, Fingerprint: "chrome"}
}

func TestLinksForAllProtocols(t *testing.T) {
	tlsS := &store.TLSSettings{SNI: "www.bing.com", PinSHA256: "pin"}
	ssS := &store.ShadowsocksSettings{Method: "2022-blake3-aes-128-gcm", ServerPSK: "c2VydmVyLXBzay0xNmJ5dA=="}
	cases := []struct {
		ib     store.Inbound
		scheme string
	}{
		{store.Inbound{Remark: "t", Protocol: "trojan", Transport: "grpc", Port: 1, Settings: store.InboundSettings{Reality: reality(), GRPC: &store.GRPCSettings{ServiceName: "g"}}}, "trojan"},
		{store.Inbound{Remark: "s", Protocol: "shadowsocks", Transport: "tcp", Port: 2, Settings: store.InboundSettings{Shadowsocks: ssS}}, "ss"},
		{store.Inbound{Remark: "u", Protocol: "tuic", Transport: "quic", Port: 3, Settings: store.InboundSettings{TLS: tlsS}}, "tuic"},
		{store.Inbound{Remark: "a", Protocol: "anytls", Transport: "tcp", Port: 4, Settings: store.InboundSettings{TLS: tlsS}}, "anytls"},
	}
	for _, c := range cases {
		link := Link(c.ib, user, "203.0.113.5")
		p, err := url.Parse(link)
		if err != nil || p.Scheme != c.scheme || p.Hostname() != "203.0.113.5" {
			t.Errorf("%s: bad link %q (%v)", c.scheme, link, err)
		}
	}

	ss := Link(cases[1].ib, user, "h")
	p, _ := url.Parse(ss)
	if pw, _ := p.User.Password(); p.User.Username() != ssS.Method || !strings.HasPrefix(pw, ssS.ServerPSK+":") {
		t.Errorf("ss userinfo must be method:serverPSK:userKey, got %q", ss)
	}

	vmess := Link(store.Inbound{Remark: "v", Protocol: "vmess", Transport: "ws", Port: 5, Settings: store.InboundSettings{WS: &store.WSSettings{Path: "/w"}}}, user, "h")
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(vmess, "vmess://"))
	var v map[string]string
	if err != nil || json.Unmarshal(raw, &v) != nil || v["net"] != "ws" || v["path"] != "/w" || v["id"] != user.UUID {
		t.Errorf("vmess link: %q", raw)
	}

	stls := store.Inbound{Protocol: "shadowtls", Settings: store.InboundSettings{ShadowTLS: &store.ShadowTLSSettings{Handshake: "x:443"}, Shadowsocks: ssS}}
	if Link(stls, user, "h") != "" {
		t.Error("ShadowTLS has no share-link format; it goes through the sing-box subscription")
	}
}

func TestSingBoxClientConfig(t *testing.T) {
	ssS := &store.ShadowsocksSettings{Method: "2022-blake3-aes-128-gcm", ServerPSK: "PSK"}
	inbounds := []store.Inbound{
		{Remark: "XHTTP", Protocol: "vless", Transport: "xhttp", Port: 443, Enabled: true, Settings: store.InboundSettings{Reality: reality(), XHTTP: &store.XHTTPSettings{Path: "/p", Mode: "auto"}}},
		{Remark: "Vision", Protocol: "vless", Transport: "tcp", Port: 8443, Enabled: true, Settings: store.InboundSettings{Reality: reality()}},
		{Remark: "STLS", Protocol: "shadowtls", Transport: "tcp", Port: 9443, Enabled: true, Settings: store.InboundSettings{ShadowTLS: &store.ShadowTLSSettings{Handshake: "www.microsoft.com:443"}, Shadowsocks: ssS}},
	}
	b, err := SingBoxConfig(inbounds, user, "203.0.113.5")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	json.Unmarshal(b, &cfg)
	byTag := map[string]map[string]any{}
	for _, o := range cfg.Outbounds {
		byTag[o["tag"].(string)] = o
	}
	if _, ok := byTag["XHTTP"]; ok {
		t.Error("sing-box has no XHTTP; it must be skipped")
	}
	if _, ok := byTag["Vision"]; ok != singboxRealityWorks {
		t.Errorf("Reality outbounds present = %v, want %v", ok, singboxRealityWorks)
	}
	if byTag["STLS"]["detour"] != "STLS (shadowtls)" || byTag["STLS (shadowtls)"]["type"] != "shadowtls" {
		t.Errorf("shadowtls chain: %v / %v", byTag["STLS"], byTag["STLS (shadowtls)"])
	}
	auto := byTag["auto"]["outbounds"].([]any)
	if len(auto) != 1 {
		t.Errorf("auto group must list only the usable methods, got %v", auto)
	}
}
