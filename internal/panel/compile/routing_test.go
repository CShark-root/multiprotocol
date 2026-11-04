package compile

import (
	"encoding/json"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"panel/internal/panel/geo"
	"panel/internal/panel/routing"
	"panel/internal/panel/store"
)

func testRouting(t *testing.T, mut func(*routing.Settings)) Routing {
	t.Helper()
	s := routing.Defaults()
	s.Presets.BlockAds = true
	s.Presets.WarpServices = []string{"openai"}
	s.WARP = routing.WarpSettings{Mode: routing.WarpRules, Account: &routing.WarpAccount{
		PrivateKey: "priv", PeerPublicKey: "peer", Endpoint: "engage.cloudflareclient.com:2408",
		Address4: "172.16.0.2/32", Address6: "2606:4700::2/128", Reserved: []int{1, 2, 3},
	}}
	s.Bridge = routing.Bridge{Port: 40000, User: "warp", Password: "secret"}
	s.Rules = []routing.Rule{
		{ID: "a", Name: "ru direct", Enabled: true, Match: []string{"domain:example.ru", "geoip:ru"}, Outbound: routing.Direct},
		{ID: "b", Name: "u7 via warp", Enabled: true, Users: []int64{7}, Outbound: routing.WARP},
		{ID: "c", Name: "off", Enabled: false, Match: []string{"domain:off.example"}, Outbound: routing.Block},
		{ID: "d", Name: "kw", Enabled: true, Match: []string{"keyword:tracker"}, Ports: "443,8000-8100", Outbound: routing.Block},
	}
	if mut != nil {
		mut(&s)
	}
	sites := map[string]geo.Site{
		"openai":           {Domains: []geo.Domain{{Kind: geo.KindSuffix, Value: "openai.com"}, {Kind: geo.KindFull, Value: "chat.openai.com"}}},
		"category-ads-all": {Domains: []geo.Domain{{Kind: geo.KindSuffix, Value: "ads.example", Attrs: []string{"ads"}}}},
	}
	ips := map[string][]netip.Prefix{"ru": {netip.MustParsePrefix("5.8.0.0/16")}}
	return NewRouting(s, "/geo", nil, sites, ips)
}

func TestXrayRouting(t *testing.T) {
	users := []store.User{{ID: 7, UUID: "11111111-1111-4111-8111-111111111111"}}
	cfg, err := Xray([]store.Inbound{inbound(1, "xhttp")}, users, "127.0.0.1:1", testRouting(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Inbounds  []map[string]any `json:"inbounds"`
		Outbounds []map[string]any `json:"outbounds"`
		Routing   struct {
			DomainStrategy string           `json:"domainStrategy"`
			Rules          []map[string]any `json:"rules"`
		} `json:"routing"`
		DNS map[string]any `json:"dns"`
	}
	json.Unmarshal(cfg, &c)
	if c.Outbounds[0]["tag"] != "direct" || !slices.ContainsFunc(c.Outbounds, func(o map[string]any) bool { return o["protocol"] == "wireguard" }) {
		t.Fatalf("outbounds: %v", c.Outbounds)
	}
	if c.Inbounds[len(c.Inbounds)-1]["tag"] != "warp-bridge" || c.Routing.Rules[0]["outboundTag"] != "warp" {
		t.Fatalf("bridge missing: %v / %v", c.Inbounds, c.Routing.Rules[0])
	}
	if c.Routing.DomainStrategy != "IPIfNonMatch" || c.DNS == nil {
		t.Fatalf("domainStrategy %q dns %v", c.Routing.DomainStrategy, c.DNS)
	}
	var tags []string
	for _, r := range c.Routing.Rules {
		b, _ := json.Marshal(r)
		tags = append(tags, string(b))
	}
	all := strings.Join(tags, "\n")
	// The mixed rule is split in two (Xray ANDs fields inside one rule).
	for _, want := range []string{
		`"domain":["domain:example.ru"],"outboundTag":"direct"`,
		`"ip":["geoip:ru"],"outboundTag":"direct"`,
		`"outboundTag":"warp","user":["u7"]`,
		`"domain":["geosite:openai"],"outboundTag":"warp"`,
		`"domain":["geosite:category-ads-all"],"outboundTag":"block"`,
		`"protocol":["bittorrent"]`,
		`"port":"443,8000-8100"`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %s in\n%s", want, all)
		}
	}
	if strings.Contains(all, "off.example") {
		t.Error("a disabled rule must not be compiled")
	}
}

func TestWarpFallsBackWhenOff(t *testing.T) {
	rt := testRouting(t, func(s *routing.Settings) { s.WARP.Mode = routing.WarpOff })
	cfg, _ := Xray([]store.Inbound{inbound(1, "xhttp")}, nil, "127.0.0.1:1", rt)
	if strings.Contains(string(cfg), "wireguard") || strings.Contains(string(cfg), `"outboundTag": "warp"`) {
		t.Fatalf("WARP off must leave no WARP outbound or rule:\n%s", cfg)
	}
	// Only WARP keeps Xray running without inbounds of its own.
	if cfg, _ := Xray(nil, nil, "127.0.0.1:1", rt); cfg != nil {
		t.Fatal("no inbounds and no WARP: Xray must stop")
	}
	if cfg, _ := Xray(nil, nil, "127.0.0.1:1", testRouting(t, nil)); cfg == nil {
		t.Fatal("the WARP bridge needs Xray even without inbounds")
	}
}

func TestWarpAllIsFinal(t *testing.T) {
	rt := testRouting(t, func(s *routing.Settings) { s.WARP.Mode = routing.WarpAll })
	cfg, _ := Xray([]store.Inbound{inbound(1, "xhttp")}, nil, "127.0.0.1:1", rt)
	var c struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	json.Unmarshal(cfg, &c)
	if c.Outbounds[0]["tag"] != "warp" {
		t.Fatalf("final outbound must come first: %v", c.Outbounds[0])
	}
}

func hyInbound() store.Inbound {
	return store.Inbound{ID: 5, Protocol: "hysteria2", Port: 8443, Enabled: true, Settings: store.InboundSettings{
		Hysteria: &store.HysteriaSettings{Masquerade: "https://example.com", CertPEM: "c", KeyPEM: "k"},
	}}
}

func TestHysteriaRouting(t *testing.T) {
	cfg, err := Hysteria([]store.Inbound{hyInbound()}, nil, testRouting(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Instances []struct {
			Server struct {
				ACL       map[string]any   `json:"acl"`
				Outbounds []map[string]any `json:"outbounds"`
				Resolver  map[string]any   `json:"resolver"`
			} `json:"server"`
		} `json:"instances"`
	}
	json.Unmarshal(cfg, &st)
	srv := st.Instances[0].Server
	var lines []string
	for _, l := range srv.ACL["inline"].([]any) {
		lines = append(lines, l.(string))
	}
	all := strings.Join(lines, "\n")
	for _, want := range []string{"reject(10.0.0.0/8)", "reject(all, tcp/25)", "direct(suffix:example.ru)", "direct(geoip:ru)", "warp(geosite:openai)", "reject(geosite:category-ads-all)"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in ACL:\n%s", want, all)
		}
	}
	// Per-user and keyword-only rules cannot be expressed: skipped, never widened.
	if strings.Contains(all, "warp(all)") || strings.Contains(all, "tracker") || strings.Contains(all, "reject(all, tcp/443)") {
		t.Errorf("rule widened in ACL:\n%s", all)
	}
	if srv.ACL["geosite"] != "/geo/geosite.dat" || srv.Outbounds[0]["name"] != "direct" || srv.Outbounds[2]["type"] != "socks5" || srv.Resolver["type"] != "https" {
		t.Errorf("acl files / outbounds / resolver: %v %v %v", srv.ACL, srv.Outbounds, srv.Resolver)
	}
}

func sbInbound() store.Inbound {
	return store.Inbound{ID: 9, Protocol: "anytls", Port: 9443, Enabled: true, Settings: store.InboundSettings{
		TLS: &store.TLSSettings{SNI: "a.example", CertPEM: "c", KeyPEM: "k"},
	}}
}

func TestSingBoxRouting(t *testing.T) {
	users := []store.User{{ID: 7, UUID: "u-7"}, {ID: 8, UUID: "u-8"}}
	rt := testRouting(t, func(s *routing.Settings) {
		s.Rules = append(s.Rules, routing.Rule{ID: "e", Enabled: true, Match: []string{"geosite:not-in-db"}, Outbound: routing.Block})
	})
	cfg, err := SingBox([]store.Inbound{sbInbound()}, users, rt)
	if err != nil {
		t.Fatal(err)
	}
	s := string(cfg)
	for _, want := range []string{`"type": "socks"`, `"warp-u7"`, `"server_port": 40000`, `"openai.com"`, `"chat.openai.com"`, `"5.8.0.0/16"`, `"ads.example"`, `"type": "https"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s", want)
		}
	}
	var c struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	json.Unmarshal(cfg, &c)
	var blocks int
	for _, r := range c.Route.Rules {
		if r["action"] == "reject" && r["rule_set"] == nil && r["protocol"] == nil {
			t.Errorf("a reject without conditions blocks everything: %v", r)
		}
		if r["action"] == "reject" {
			blocks++
		}
		// The per-user WARP rule routes only u7.
		if r["outbound"] == "warp-u8" && r["rule_set"] == nil {
			t.Errorf("u8 must not get u7's catch-all WARP rule: %v", r)
		}
	}
	// private, smtp, bittorrent, ads, keyword; the unknown category is dropped.
	if blocks != 5 {
		t.Errorf("want 5 reject rules, got %d", blocks)
	}
}

func TestRoutingNormalize(t *testing.T) {
	ok := func(int64) bool { return true }
	s := routing.Defaults()
	s.Rules = []routing.Rule{{Match: []string{" Example.COM ", "1.2.3.0/24", "2001:db8::1", "geosite:OpenAI", ""}, Outbound: "direct", Enabled: true}}
	got, err := routing.Normalize(s, ok)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"domain:example.com", "1.2.3.0/24", "2001:db8::1", "geosite:openai"}
	if !slices.Equal(got.Rules[0].Match, want) || got.Rules[0].ID == "" {
		t.Fatalf("got %v (id %q)", got.Rules[0].Match, got.Rules[0].ID)
	}
	for _, bad := range []routing.Rule{
		{Match: []string{"bad domain"}, Outbound: "direct"},
		{Match: []string{"x.com"}, Outbound: "nowhere"},
		{Match: []string{"x.com"}, Ports: "70000", Outbound: "direct"},
		{Outbound: "block"}, // matches nothing: would block everything
		{Match: []string{"regexp:("}, Outbound: "direct"},
	} {
		s.Rules = []routing.Rule{bad}
		if _, err := routing.Normalize(s, ok); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestCountriesAndMissingCategories(t *testing.T) {
	s := routing.Defaults()
	s.Presets.Countries = []routing.CountryRule{{Code: "ru", Outbound: routing.WARP}, {Code: "zz", Outbound: routing.Block}}
	s.WARP = routing.WarpSettings{Mode: routing.WarpRules, Account: &routing.WarpAccount{Endpoint: "162.159.192.1:2408"}}
	s.Bridge = routing.Bridge{Port: 40000, User: "u", Password: "p"}
	s.Rules = []routing.Rule{{ID: "x", Enabled: true, Match: []string{"geosite:typo-category"}, Outbound: routing.Block}}
	// The database has Russia's IP ranges and sites but nothing for "zz"
	// and no "typo-category".
	has := func(e string) bool { return e == "geoip:ru" || e == "geosite:category-ru" }
	rt := NewRouting(s, "/geo", has, nil, nil)
	var names []string
	for _, r := range rt.Rules {
		names = append(names, r.Name)
	}
	if !slices.Contains(names, "country:ru") || slices.Contains(names, "country:zz") || slices.Contains(names, "") {
		t.Fatalf("rules: %v", names)
	}
	cfg, err := Xray([]store.Inbound{inbound(1, "xhttp")}, nil, "127.0.0.1:1", rt)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"geosite:category-ru"`, `"geoip:ru"`} {
		if !strings.Contains(string(cfg), want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(string(cfg), "typo-category") || strings.Contains(string(cfg), "geoip:zz") {
		t.Error("unknown categories must not reach Xray: it would refuse to start")
	}
	// Country codes are validated.
	s.Presets.Countries = []routing.CountryRule{{Code: "russia", Outbound: routing.WARP}}
	if _, err := routing.Normalize(s, func(int64) bool { return true }); err == nil {
		t.Error("accepted a bad country code")
	}
}

func TestWarpBridgeWithoutRules(t *testing.T) {
	// WARP just registered, nothing routed through it yet: the bridge must
	// still be there so the panel's WARP check works.
	rt := testRouting(t, func(s *routing.Settings) {
		s.Rules = nil
		s.Presets.WarpServices = nil
	})
	cfg, _ := Xray(nil, nil, "127.0.0.1:1", rt)
	if cfg == nil || !strings.Contains(string(cfg), `"warp-bridge"`) || !strings.Contains(string(cfg), `"wireguard"`) {
		t.Fatalf("no bridge:\n%s", cfg)
	}
}

func TestExtraDNS(t *testing.T) {
	rt := testRouting(t, func(s *routing.Settings) {
		s.DNS.Extra = []routing.DNSExtra{{Server: "yandex", Domains: []string{"domain:ru", "geosite:category-ru", "geosite:missing"}}}
		s.DNS.Hosts = []routing.DNSHost{{Name: "router.lan", IP: "192.168.1.1"}}
		s.DNS.DisableFallbackIfMatch = true
	})
	rt.DNS = dnsAvailable(rt.DNS, func(e string) bool { return e == "geosite:category-ru" })
	rt.Sites["category-ru"] = geo.Site{Domains: []geo.Domain{{Kind: geo.KindSuffix, Value: "ya.ru"}}}
	x, _ := Xray([]store.Inbound{inbound(1, "xhttp")}, nil, "127.0.0.1:1", rt)
	for _, want := range []string{`"address": "77.88.8.8"`, `"geosite:category-ru"`, `"disableFallbackIfMatch": true`, `"router.lan": "192.168.1.1"`} {
		if !strings.Contains(string(x), want) {
			t.Errorf("xray: missing %s", want)
		}
	}
	if strings.Contains(string(x), "geosite:missing") {
		t.Error("an unknown category must not reach Xray")
	}
	sb, _ := SingBox([]store.Inbound{sbInbound()}, []store.User{{ID: 7, UUID: "u"}}, rt)
	for _, want := range []string{`"tag": "dns-1"`, `"server": "77.88.8.8"`, `"ya.ru"`, `"type": "hosts"`} {
		if !strings.Contains(string(sb), want) {
			t.Errorf("sing-box: missing %s", want)
		}
	}
	// Validation: an extra server needs domains, hosts need real addresses.
	s := routing.Defaults()
	s.DNS.Extra = []routing.DNSExtra{{Server: "yandex"}}
	if _, err := routing.Normalize(s, func(int64) bool { return true }); err == nil {
		t.Error("accepted an extra server without domains")
	}
	s.DNS.Extra = nil
	s.DNS.Hosts = []routing.DNSHost{{Name: "a.lan", IP: "nope"}}
	if _, err := routing.Normalize(s, func(int64) bool { return true }); err == nil {
		t.Error("accepted a bad hosts address")
	}
}

func TestCustomOutbound(t *testing.T) {
	s := routing.Defaults()
	s.Outbounds = []routing.CustomOutbound{{ID: "ab12", Name: "Second VPS", Link: "socks5://u:p@5.6.7.8:1080",
		Bridge: routing.Bridge{Port: 41000, User: "out", Password: "pw"}}}
	s.Rules = []routing.Rule{{ID: "r", Enabled: true, Match: []string{"domain:example.com"}, Outbound: "out:ab12"},
		{ID: "x", Enabled: true, Match: []string{"domain:gone.example"}, Outbound: "out:gone"}}
	rt := NewRouting(s, "/geo", nil, nil, nil)
	x, _ := Xray(nil, nil, "127.0.0.1:1", rt)
	for _, want := range []string{`"tag": "out_ab12"`, `"protocol": "socks"`, `"tag": "bridge_out_ab12"`, `"outboundTag": "out_ab12"`} {
		if !strings.Contains(string(x), want) {
			t.Errorf("xray: missing %s", want)
		}
	}
	if strings.Contains(string(x), "out_gone") {
		t.Error("a rule naming a deleted exit must go direct")
	}
	hy, _ := Hysteria([]store.Inbound{hyInbound()}, nil, rt)
	if !strings.Contains(string(hy), `"name": "out_ab12"`) || !strings.Contains(string(hy), "out_ab12(suffix:example.com)") {
		t.Errorf("hysteria:\n%s", hy)
	}
	sb, _ := SingBox([]store.Inbound{sbInbound()}, []store.User{{ID: 7, UUID: "u"}}, rt)
	if !strings.Contains(string(sb), `"tag": "out_ab12-u7"`) || !strings.Contains(string(sb), `"server_port": 41000`) {
		t.Errorf("sing-box: no per-user exit")
	}
	// Rules may only name existing exits.
	s.Rules = []routing.Rule{{ID: "r", Enabled: true, Match: []string{"x.com"}, Outbound: "out:nope"}}
	if _, err := routing.Normalize(s, func(int64) bool { return true }); err == nil {
		t.Error("accepted a rule to an unknown exit")
	}
}
