package compile

import (
	"encoding/json"
	"strings"
	"testing"

	"panel/internal/agent/hysteria"
	"panel/internal/panel/store"
)

func TestHysteriaState(t *testing.T) {
	hy := store.Inbound{
		ID: 5, Protocol: "hysteria2", Transport: "quic", Port: 443, Enabled: true,
		Settings: store.InboundSettings{Hysteria: &store.HysteriaSettings{
			SNI: "www.bing.com", CertPEM: "CERT", KeyPEM: "KEY", Masquerade: "https://www.bing.com/",
			ObfsPassword: "secret", UpMbps: 100,
		}},
	}
	users := []store.User{{ID: 3, UUID: "uuid-3"}}

	if cfg, _ := Hysteria([]store.Inbound{inbound(1, "xhttp")}, users, DefaultRouting()); cfg != nil {
		t.Fatal("VLESS inbounds must not produce a Hysteria state")
	}
	cfg, err := Hysteria([]store.Inbound{hy, inbound(1, "xhttp")}, users, DefaultRouting())
	if err != nil {
		t.Fatal(err)
	}
	var st hysteria.State
	if err := json.Unmarshal(cfg, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Instances) != 1 || st.Instances[0].Tag != "in-5" || st.Instances[0].CertPEM != "CERT" {
		t.Fatalf("unexpected instances: %+v", st.Instances)
	}
	if st.Users["uuid-3"] != "u3" {
		t.Fatalf("users table: %v", st.Users)
	}
	s := st.Instances[0].Server
	if s["listen"] != ":443" {
		t.Errorf("listen = %v", s["listen"])
	}
	if _, ok := s["obfs"]; !ok {
		t.Error("obfs missing")
	}
	if bw := s["bandwidth"].(map[string]any); bw["up"] != "100 mbps" || bw["down"] != nil {
		t.Errorf("bandwidth = %v", bw)
	}
	acl, _ := json.Marshal(s["acl"])
	if !strings.Contains(string(acl), "reject(10.0.0.0/8)") || !strings.Contains(string(acl), "tcp/25") {
		t.Errorf("acl = %s", acl)
	}
	if _, ok := s["auth"]; ok {
		t.Error("auth is node-local and must be filled in by the agent")
	}
}
