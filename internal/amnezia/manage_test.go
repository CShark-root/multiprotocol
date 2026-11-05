package amnezia

import (
	"encoding/json"
	"strings"
	"testing"
)

// A config as the Amnezia app writes it (configure_container.sh plus peers
// appended by the app), including the commented client-only I1.
const appConf = `[Interface]
PrivateKey = SGVsbG9IZWxsb0hlbGxvSGVsbG9IZWxsb0hlbGxvSGU=
Address = 10.8.1.0/24
ListenPort = 51820
Jc = 4
Jmin = 10
Jmax = 50
S1 = 91
S2 = 76
H1 = 1100000001-1100000100
H2 = 1200000001-1200000100
H3 = 1300000001-1300000100
H4 = 1400000001-1400000100
# I1 = <r 2><b 0x8580>
[Peer]
PublicKey = uTH3FqoaxNRqh/r5z+uvQsic9urabKvl63X3J8X3IF8=
PresharedKey = cHNrcHNrcHNrcHNrcHNrcHNrcHNrcHNrcHNrcHNrcHM=
AllowedIPs = 10.8.1.1/32

[Peer]
PublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
PresharedKey = cHNrcHNrcHNrcHNrcHNrcHNrcHNrcHNrcHNrcHNrcHM=
AllowedIPs = 10.8.1.2/32

`

func TestParseConf(t *testing.T) {
	c, err := parseConf(appConf)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.peers) != 2 || c.port != 51820 || c.subnet.String() != "10.8.1.0/24" {
		t.Fatalf("parsed %+v", c)
	}
	if c.iface["#I1"] != "<r 2><b 0x8580>" {
		t.Errorf("commented I1 = %q", c.iface["#I1"])
	}
	ip, err := c.nextIP()
	if err != nil || ip.String() != "10.8.1.3" {
		t.Errorf("next ip %v %v", ip, err)
	}
}

func TestParseConfRefusesUnknown(t *testing.T) {
	for _, bad := range []string{
		strings.Replace(appConf, "Jc = 4", "NewThing = 1", 1),
		strings.Replace(appConf, "AllowedIPs = 10.8.1.2/32", "AllowedIPs = nonsense", 1),
		strings.Replace(appConf, "[Interface]", "", 1),
		strings.Replace(appConf, "PublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "PublicKey = uTH3FqoaxNRqh/r5z+uvQsic9urabKvl63X3J8X3IF8=", 1),
		"garbage",
	} {
		if _, err := parseConf(bad); err == nil {
			t.Errorf("accepted:\n%s", bad)
		}
	}
}

func TestWithoutPeer(t *testing.T) {
	out, ok := withoutPeer(appConf, "uTH3FqoaxNRqh/r5z+uvQsic9urabKvl63X3J8X3IF8=")
	if !ok {
		t.Fatal("not found")
	}
	c, err := parseConf(out)
	if err != nil || len(c.peers) != 1 || c.peers[0].key != "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" {
		t.Fatalf("after removal: %v\n%s", err, out)
	}
	// Everything but the removed block stays byte for byte.
	if !strings.HasPrefix(out, appConf[:strings.Index(appConf, "[Peer]")]) || !strings.HasSuffix(out, appConf[strings.LastIndex(appConf, "[Peer]"):]) {
		t.Errorf("text changed:\n%s", out)
	}
	if _, ok := withoutPeer(appConf, "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB="); ok {
		t.Error("removed a peer that is not there")
	}
}

func TestClientConfig(t *testing.T) {
	c, _ := parseConf(appConf)
	priv, pub, err := genKey()
	if err != nil || !validKey(priv) || !validKey(pub) {
		t.Fatal("bad keys", err)
	}
	ip, _ := c.nextIP()
	cfg := clientConfig(c, priv, ip, "1.1.1.1, 1.0.0.1", pub, "cHNrcHNrcHNrcHNrcHNrcHNrcHNrcHNrcHNrcHNrcHM=", "vpn.example.org", 34813)
	for _, want := range []string{"Address = 10.8.1.3/32", "Jc = 4", "H4 = 1400000001-1400000100", "I1 = <r 2><b 0x8580>", "Endpoint = vpn.example.org:34813", "PrivateKey = " + priv} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config lacks %q:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "ListenPort") || strings.Contains(cfg, "SGVsbG9IZWxs") {
		t.Errorf("server-only values leaked into the client config:\n%s", cfg)
	}
}

func TestVersion(t *testing.T) {
	c, _ := parseConf(appConf)
	if v := c.Version(); v != "2" {
		t.Errorf("H ranges: version %q, want 2", v)
	}
	c3, _ := parseConf(strings.Replace(appConf, "Jc = 4", "Jc = 4\nHeaderProtectionKey = abc", 1))
	if v := c3.Version(); v != "3.1" {
		t.Errorf("AWG 3 params: version %q, want 3.1", v)
	}
	c15, _ := parseConf(strings.NewReplacer("1100000001-1100000100", "11", "1200000001-1200000100", "12", "1300000001-1300000100", "13", "1400000001-1400000100", "14").Replace(appConf))
	if v := c15.Version(); v != "1.5" {
		t.Errorf("I1 only: version %q, want 1.5", v)
	}
}

func TestVPNKeyRoundTrip(t *testing.T) {
	c, _ := parseConf(appConf)
	key, err := vpnKey(c, "[Interface]\nPrivateKey = x\n", clientData{
		container: "amnezia-awg2", description: "MultiProtocol · Тест", host: "vpn.example.org", ip: "10.8.1.3",
		priv: "p", pub: "P", serverPub: "S", psk: "K", dns: "1.1.1.1, 1.0.0.1", port: 34813,
	})
	if err != nil || !strings.HasPrefix(key, "vpn://") || strings.ContainsAny(key[6:], "+/=") {
		t.Fatalf("key %q %v", key, err)
	}
	body, err := DecodeVPNKey(key)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Containers []struct {
			Container string            `json:"container"`
			AWG       map[string]string `json:"awg"`
		} `json:"containers"`
		Default string `json:"defaultContainer"`
		Host    string `json:"hostName"`
		DNS1    string `json:"dns1"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	a := doc.Containers[0].AWG
	if doc.Default != "amnezia-awg2" || doc.Host != "vpn.example.org" || doc.DNS1 != "1.1.1.1" || a["port"] != "34813" || a["H1"] != "1100000001-1100000100" || a["I1"] != "<r 2><b 0x8580>" {
		t.Errorf("decoded %s", body)
	}
	var last map[string]any
	if err := json.Unmarshal([]byte(a["last_config"]), &last); err != nil || last["client_ip"] != "10.8.1.3" || last["port"] != float64(34813) {
		t.Errorf("last_config %v %v", last, err)
	}
}
