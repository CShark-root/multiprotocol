package compile

import (
	"encoding/json"
	"testing"

	"panel/internal/panel/store"
)

var testUser = store.User{ID: 7, UUID: "0123abcd-4567-4890-abcd-ef0123456789"}

func TestXrayTrojanVMessShadowsocks(t *testing.T) {
	trojan := inbound(1, "tcp")
	trojan.Protocol = "trojan"
	vmess := store.Inbound{ID: 2, Protocol: "vmess", Transport: "ws", Port: 2000, Enabled: true,
		Settings: store.InboundSettings{WS: &store.WSSettings{Path: "/ws"}}}
	ss := store.Inbound{ID: 3, Protocol: "shadowsocks", Transport: "tcp", Port: 3000, Enabled: true,
		Settings: store.InboundSettings{Shadowsocks: &store.ShadowsocksSettings{Method: "2022-blake3-aes-128-gcm", ServerPSK: "PSK"}}}

	cfg, err := Xray([]store.Inbound{trojan, vmess, ss}, []store.User{testUser}, "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	json.Unmarshal(cfg, &parsed)
	if len(parsed.Inbounds) != 3 {
		t.Fatalf("want 3 inbounds, got %d", len(parsed.Inbounds))
	}
	client := func(i int) map[string]any {
		return parsed.Inbounds[i]["settings"].(map[string]any)["clients"].([]any)[0].(map[string]any)
	}

	if parsed.Inbounds[0]["protocol"] != "trojan" || client(0)["password"] != testUser.UUID || client(0)["flow"] != nil {
		t.Errorf("trojan: %v", parsed.Inbounds[0])
	}
	if s := parsed.Inbounds[0]["streamSettings"].(map[string]any); s["security"] != "reality" {
		t.Errorf("trojan must use Reality: %v", s)
	}
	if s := parsed.Inbounds[1]["streamSettings"].(map[string]any); s["network"] != "ws" || s["security"] != "none" {
		t.Errorf("vmess stream: %v", s)
	}
	ssSettings := parsed.Inbounds[2]["settings"].(map[string]any)
	if ssSettings["password"] != "PSK" || ssSettings["network"] != "tcp,udp" || client(2)["password"] != SSUserKey(testUser.UUID) {
		t.Errorf("shadowsocks: %v", ssSettings)
	}
	if SSUserKey(testUser.UUID) == "" || SSUserKey("not-a-uuid") != "" {
		t.Error("SSUserKey must derive a 16-byte key from a UUID only")
	}
}

func TestSingBoxConfig(t *testing.T) {
	tlsSettings := &store.TLSSettings{SNI: "www.bing.com", CertPEM: "CERT", KeyPEM: "KEY"}
	tuic := store.Inbound{ID: 1, Protocol: "tuic", Port: 1000, Enabled: true, Settings: store.InboundSettings{TLS: tlsSettings}}
	anytls := store.Inbound{ID: 2, Protocol: "anytls", Port: 2000, Enabled: true, Settings: store.InboundSettings{TLS: tlsSettings}}
	stls := store.Inbound{ID: 3, Protocol: "shadowtls", Port: 3000, Enabled: true, Settings: store.InboundSettings{
		ShadowTLS:   &store.ShadowTLSSettings{Handshake: "www.microsoft.com:443"},
		Shadowsocks: &store.ShadowsocksSettings{Method: "2022-blake3-aes-128-gcm", ServerPSK: "PSK"},
	}}

	if cfg, _ := SingBox([]store.Inbound{inbound(9, "xhttp")}, nil); cfg != nil {
		t.Fatal("Xray inbounds must not produce a sing-box config")
	}
	cfg, err := SingBox([]store.Inbound{tuic, anytls, stls}, []store.User{testUser})
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Inbounds  []map[string]any `json:"inbounds"`
		Outbounds []map[string]any `json:"outbounds"`
		Route     struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	json.Unmarshal(cfg, &parsed)

	types := []string{}
	for _, in := range parsed.Inbounds {
		types = append(types, in["type"].(string))
	}
	want := []string{"tuic", "anytls", "shadowtls", "shadowsocks"}
	if len(types) != len(want) {
		t.Fatalf("inbound types %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("inbound types %v, want %v", types, want)
		}
	}
	if parsed.Inbounds[2]["detour"] != parsed.Inbounds[3]["tag"] {
		t.Error("ShadowTLS must hand over to its inner Shadowsocks inbound")
	}
	if parsed.Inbounds[3]["listen"] != "127.0.0.1" {
		t.Error("the inner Shadowsocks inbound must stay on loopback")
	}

	// Per-user outbound so the agent can attribute traffic.
	var routed bool
	for _, r := range parsed.Route.Rules {
		if users, ok := r["auth_user"].([]any); ok && users[0] == "u7" && r["outbound"] == "direct-u7" {
			routed = true
		}
	}
	if !routed {
		t.Errorf("no per-user route rule: %v", parsed.Route.Rules)
	}
	if first := parsed.Route.Rules[0]; first["action"] != "reject" {
		t.Errorf("private ranges must be rejected first: %v", first)
	}
}
