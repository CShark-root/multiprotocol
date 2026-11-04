package outbound

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	ss := "ss://" + base64.RawURLEncoding.EncodeToString([]byte("2022-blake3-aes-128-gcm:psk")) + "@1.2.3.4:8388#name"
	vm, _ := json.Marshal(map[string]any{"add": "v.example", "port": "443", "id": "11111111-1111-4111-8111-111111111111", "net": "ws", "path": "/w", "tls": "tls"})
	cases := map[string]string{
		"vless://11111111-1111-4111-8111-111111111111@r.example:443?type=xhttp&security=reality&sni=www.microsoft.com&pbk=PUB&sid=ab&path=%2Fx#n": `"publicKey":"PUB"`,
		"trojan://secret@t.example:443?security=tls&sni=t.example&type=ws&path=%2Fws":                                                             `"password":"secret"`,
		ss: `"method":"2022-blake3-aes-128-gcm"`,
		"vmess://" + base64.StdEncoding.EncodeToString(vm): `"wsSettings":{"host":"","path":"/w"}`,
		"socks5://u:p@5.6.7.8:1080":                        `"user":"u"`,
		"http://9.9.9.9:3128":                              `"protocol":"http"`,
	}
	for link, want := range cases {
		p, err := Parse(link)
		if err != nil {
			t.Errorf("%s: %v", link[:12], err)
			continue
		}
		b, _ := json.Marshal(p)
		if !strings.Contains(string(b), want) {
			t.Errorf("%s: missing %s in %s", link[:12], want, b)
		}
	}
	for _, bad := range []string{"https://", "hysteria2://x@h:1", "vless://@h:443", "socks5://h", "nonsense"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
