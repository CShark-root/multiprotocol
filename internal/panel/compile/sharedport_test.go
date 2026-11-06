package compile

import (
	"encoding/json"
	"testing"

	"panel/internal/agent/sniproxy"
	"panel/internal/panel/store"
)

func TestSharePorts(t *testing.T) {
	re := func(sni string) store.InboundSettings {
		return store.InboundSettings{Reality: &store.RealitySettings{ServerNames: []string{sni}, Target: sni + ":443"}}
	}
	list := []store.Inbound{
		{ID: 1, Protocol: "vless", Port: 443, Enabled: true, Settings: re("www.microsoft.com")},
		{ID: 2, Protocol: "vless-sb", Port: 443, Enabled: true, Settings: re("www.apple.com")},
		{ID: 3, Protocol: "vless-sb", Port: 8443, Enabled: true, Settings: re("www.apple.com")}, // своего Xray нет — сам
	}
	out, cfg := SharePorts(list)
	if cfg == nil {
		t.Fatal("no splitter config")
	}
	var c sniproxy.Config
	json.Unmarshal(cfg, &c)
	if len(c.Listeners) != 1 || c.Listeners[0].Listen != ":443" || len(c.Listeners[0].Routes) != 1 || c.Listeners[0].Routes[0].SNI != "www.apple.com" || !c.Listeners[0].Default.ProxyProtocol {
		t.Fatalf("bad config: %s", cfg)
	}
	if !out[0].LocalOnly || out[0].Port == 443 || out[0].Settings.Sockopt == nil || !out[0].Settings.Sockopt.AcceptProxyProtocol {
		t.Fatalf("xray inbound not moved behind the splitter: %+v", out[0])
	}
	if !out[1].LocalOnly || out[1].Port == 443 || out[2].LocalOnly || out[2].Port != 8443 {
		t.Fatalf("sing-box inbounds wrong: %+v %+v", out[1], out[2])
	}
	if list[0].Port != 443 || list[0].Settings.Sockopt != nil {
		t.Fatal("SharePorts changed the input")
	}
	if _, cfg := SharePorts(list[:1]); cfg != nil {
		t.Fatal("splitter without vless-sb")
	}
}
