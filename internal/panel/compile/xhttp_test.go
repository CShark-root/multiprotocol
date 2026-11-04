package compile

import (
	"encoding/json"
	"testing"

	"panel/internal/panel/store"
)

func TestXHTTPServerAndClientSplit(t *testing.T) {
	x := &store.XHTTPSettings{
		Path: "/p", Mode: "packet-up", Host: "cdn.example.com",
		Headers: map[string]string{"X-Test": "1"}, PaddingBytes: "100-1000", PaddingObfsMode: true,
		UplinkHTTPMethod: "PUT", SessionPlacement: "header", MaxBufferedPosts: 30, NoSSEHeader: true,
		MaxEachPostBytes: "1000000", MinPostsIntervalMs: "10-50",
		Xmux: &store.XmuxSettings{MaxConcurrency: "16-32", HKeepAlivePeriod: 30},
	}
	server, client := XHTTPServer(x), XHTTPClientExtra(x)

	for _, k := range []string{"host", "headers", "xPaddingBytes", "xPaddingObfsMode", "uplinkHTTPMethod", "sessionIDPlacement"} {
		if server[k] == nil || client[k] == nil {
			t.Errorf("%s must be on both sides: server=%v client=%v", k, server[k], client[k])
		}
	}
	if server["scMaxBufferedPosts"] != 30 || server["noSSEHeader"] != true || client["scMaxBufferedPosts"] != nil {
		t.Errorf("server-only settings misplaced: %v / %v", server, client)
	}
	if client["xmux"] == nil || client["scMinPostsIntervalMs"] != "10-50" || server["xmux"] != nil {
		t.Errorf("client-only settings misplaced: %v / %v", server, client)
	}
	if server["scMaxEachPostBytes"] != 1000000 {
		t.Errorf("a single number must be emitted as a number, got %#v", server["scMaxEachPostBytes"])
	}
	if XHTTPClientExtra(&store.XHTTPSettings{Path: "/p", Mode: "auto"}) != nil {
		t.Error("defaults must not produce an extra parameter")
	}
}

func TestRealityExpertInXray(t *testing.T) {
	ib := inbound(1, "tcp")
	ib.Settings.Reality.Xver, ib.Settings.Reality.MaxTimeDiff = 1, 60000
	ib.Settings.Reality.MinClientVer, ib.Settings.Reality.MLDSA65Seed = "25.9.11", "SEED"
	ib.Settings.Sniffing = &store.SniffingSettings{Enabled: true, DestOverride: []string{"tls"}, MetadataOnly: true}
	ib.Settings.Sockopt = &store.SockoptSettings{TCPFastOpen: true, TCPCongestion: "bbr"}
	cfg, _ := Xray([]store.Inbound{ib}, nil, "127.0.0.1:1")
	var parsed struct {
		Inbounds []struct {
			Sniffing       map[string]any `json:"sniffing"`
			StreamSettings struct {
				Reality map[string]any `json:"realitySettings"`
				Sockopt map[string]any `json:"sockopt"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
	}
	json.Unmarshal(cfg, &parsed)
	in := parsed.Inbounds[0]
	r := in.StreamSettings.Reality
	if r["xver"] != 1.0 || r["maxTimeDiff"] != 60000.0 || r["minClientVer"] != "25.9.11" || r["mldsa65Seed"] != "SEED" {
		t.Errorf("reality: %v", r)
	}
	if in.Sniffing["metadataOnly"] != true || in.StreamSettings.Sockopt["tcpCongestion"] != "bbr" {
		t.Errorf("sniffing/sockopt: %v / %v", in.Sniffing, in.StreamSettings.Sockopt)
	}
}
