package subscription

import (
	"net/url"
	"strings"
	"testing"

	"panel/internal/panel/store"
)

func TestVLESSLinkXHTTP(t *testing.T) {
	ib := store.Inbound{
		Remark: "Основной", Protocol: "vless", Transport: "xhttp", Port: 443, Enabled: true,
		Settings: store.InboundSettings{
			Reality: &store.RealitySettings{ServerNames: []string{"www.microsoft.com"}, PublicKey: "PUB", ShortIDs: []string{"ab12"}, Fingerprint: "chrome"},
			XHTTP:   &store.XHTTPSettings{Path: "/x1", Mode: "auto"},
		},
	}
	u := store.User{Name: "телефон", UUID: "11111111-1111-4111-8111-111111111111"}
	link := VLESSLink(ib, u, "203.0.113.5")

	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "vless" || parsed.User.Username() != u.UUID || parsed.Host != "203.0.113.5:443" {
		t.Fatalf("bad link head: %s", link)
	}
	q := parsed.Query()
	want := map[string]string{
		"type": "xhttp", "path": "/x1", "mode": "auto", "security": "reality",
		"sni": "www.microsoft.com", "pbk": "PUB", "sid": "ab12", "fp": "chrome", "encryption": "none",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if q.Has("flow") {
		t.Error("XHTTP links must not carry a Vision flow")
	}
	if parsed.Fragment != "Основной · телефон" {
		t.Errorf("fragment %q", parsed.Fragment)
	}

	ib.Address = "vpn.example.com"
	if !strings.Contains(VLESSLink(ib, u, "203.0.113.5"), "@vpn.example.com:443") {
		t.Error("inbound address must override the default host")
	}
}

func TestHysteria2Link(t *testing.T) {
	ib := store.Inbound{
		Remark: "Hy2", Protocol: "hysteria2", Transport: "quic", Port: 443, Enabled: true,
		Settings: store.InboundSettings{Hysteria: &store.HysteriaSettings{
			SNI: "www.bing.com", PinSHA256: "abcd", ObfsPassword: "ob",
		}},
	}
	u := store.User{Name: "phone", UUID: "11111111-1111-4111-8111-111111111111"}
	link := Link(ib, u, "203.0.113.5")
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "hysteria2" || parsed.User.Username() != u.UUID || parsed.Host != "203.0.113.5:443" {
		t.Fatalf("bad link head: %s", link)
	}
	q := parsed.Query()
	for k, v := range map[string]string{"sni": "www.bing.com", "insecure": "1", "pinSHA256": "abcd", "obfs": "salamander", "obfs-password": "ob"} {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	ib.Settings.Hysteria.ObfsPassword = ""
	if strings.Contains(Link(ib, u, "h"), "obfs") {
		t.Error("obfs params must be absent when obfs is off")
	}
}
