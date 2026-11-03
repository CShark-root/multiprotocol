package compile

import (
	"bytes"
	"encoding/json"
	"testing"

	"panel/internal/panel/store"
)

func inbound(id int64, transport string) store.Inbound {
	ib := store.Inbound{
		ID: id, Remark: "r", Protocol: "vless", Transport: transport, Security: "reality", Port: 443 + int(id), Enabled: true,
		Settings: store.InboundSettings{Reality: &store.RealitySettings{
			Target: "www.microsoft.com:443", ServerNames: []string{"www.microsoft.com"},
			PrivateKey: "priv", PublicKey: "pub", ShortIDs: []string{"abcd"}, Fingerprint: "chrome",
		}},
	}
	switch transport {
	case "xhttp":
		ib.Settings.XHTTP = &store.XHTTPSettings{Path: "/p", Mode: "auto"}
	case "grpc":
		ib.Settings.GRPC = &store.GRPCSettings{ServiceName: "svc"}
	}
	return ib
}

func TestXrayNoInboundsMeansStop(t *testing.T) {
	disabled := inbound(1, "xhttp")
	disabled.Enabled = false
	cfg, err := Xray([]store.Inbound{disabled}, nil, "127.0.0.1:1")
	if err != nil || cfg != nil {
		t.Fatalf("want nil config, got %s, %v", cfg, err)
	}
}

func TestXrayConfig(t *testing.T) {
	users := []store.User{{ID: 7, UUID: "11111111-1111-4111-8111-111111111111"}}
	ins := []store.Inbound{inbound(1, "xhttp"), inbound(2, "tcp"), inbound(3, "grpc")}
	cfg, err := Xray(ins, users, "127.0.0.1:10085")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Xray(ins, users, "127.0.0.1:10085")
	if !bytes.Equal(cfg, again) {
		t.Fatal("output must be deterministic")
	}

	var parsed struct {
		Inbounds []struct {
			Port     int `json:"port"`
			Settings struct {
				Clients []map[string]string `json:"clients"`
			} `json:"settings"`
			StreamSettings struct {
				Network string `json:"network"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Inbounds) != 3 {
		t.Fatalf("want 3 inbounds, got %d", len(parsed.Inbounds))
	}
	wantNet := []string{"xhttp", "raw", "grpc"}
	for i, in := range parsed.Inbounds {
		if in.StreamSettings.Network != wantNet[i] {
			t.Errorf("inbound %d: network %q, want %q", i, in.StreamSettings.Network, wantNet[i])
		}
		c := in.Settings.Clients[0]
		if c["email"] != "u7" || c["id"] != users[0].UUID {
			t.Errorf("inbound %d: bad client %v", i, c)
		}
		if (c["flow"] == "xtls-rprx-vision") != (wantNet[i] == "raw") {
			t.Errorf("inbound %d: Vision flow must be set only for TCP, got %q", i, c["flow"])
		}
	}
}

func TestEmailRoundTrip(t *testing.T) {
	if id, ok := UserIDFromEmail(Email(42)); !ok || id != 42 {
		t.Fatalf("round trip failed: %d %v", id, ok)
	}
	for _, bad := range []string{"", "u", "x1", "u-1", "u0", "uabc"} {
		if _, ok := UserIDFromEmail(bad); ok {
			t.Errorf("%q must not parse", bad)
		}
	}
}
