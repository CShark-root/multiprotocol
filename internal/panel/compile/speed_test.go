package compile

import (
	"encoding/json"
	"testing"

	"panel/internal/panel/store"
)

func TestPerUserSpeedMarks(t *testing.T) {
	users := []store.User{
		{ID: 7, UUID: "11111111-1111-4111-8111-111111111111", SpeedLimitMbps: 5},
		{ID: 8, UUID: "22222222-2222-4222-8222-222222222222"},
	}
	xrayCfg, err := Xray([]store.Inbound{inbound(1, "xhttp")}, users, "127.0.0.1:1", DefaultRouting())
	if err != nil {
		t.Fatal(err)
	}
	var xr struct {
		Outbounds []map[string]any `json:"outbounds"`
		Routing   struct {
			Rules []map[string]any `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(xrayCfg, &xr); err != nil {
		t.Fatal(err)
	}
	marked := false
	for _, o := range xr.Outbounds {
		if o["tag"] != "speed-u7-direct" {
			continue
		}
		stream := o["streamSettings"].(map[string]any)
		marked = stream["sockopt"].(map[string]any)["mark"] == float64(SpeedMark(7))
	}
	if !marked {
		t.Fatal("Xray has no marked outbound for the limited user")
	}
	routed := false
	for _, r := range xr.Routing.Rules {
		if r["outboundTag"] == "speed-u7-direct" {
			users := r["user"].([]any)
			routed = len(users) == 1 && users[0] == "u7"
		}
	}
	if !routed {
		t.Fatalf("Xray has no per-user speed route: %+v", xr.Routing.Rules)
	}

	ib := inbound(1, "xhttp")
	ib.Protocol = "tuic"
	ib.Settings.TLS = &store.TLSSettings{SNI: "example.com", CertPEM: "cert", KeyPEM: "key"}
	sbCfg, err := SingBox([]store.Inbound{ib}, users, DefaultRouting())
	if err != nil {
		t.Fatal(err)
	}
	var sb struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	json.Unmarshal(sbCfg, &sb)
	for _, o := range sb.Outbounds {
		if o["tag"] == "direct-u7" && o["routing_mark"] != float64(SpeedMark(7)) {
			t.Errorf("sing-box limited outbound: %v", o)
		}
		if o["tag"] == "direct-u8" && o["routing_mark"] != nil {
			t.Errorf("unlimited user received a mark: %v", o)
		}
	}
}

func TestEffectiveSpeedAfterQuota(t *testing.T) {
	u := store.User{TrafficLimit: 100, TrafficUsed: 100, SpeedLimitMbps: 50, QuotaAction: "throttle", ThrottledSpeedMbps: 5}
	if got := u.EffectiveSpeedMbps(); got != 5 {
		t.Fatalf("effective speed %d", got)
	}
	u.TrafficUsed = 99
	if got := u.EffectiveSpeedMbps(); got != 50 {
		t.Fatalf("speed before quota %d", got)
	}
}
