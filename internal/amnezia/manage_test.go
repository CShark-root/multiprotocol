package amnezia

import (
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
