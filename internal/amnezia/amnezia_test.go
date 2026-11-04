package amnezia

import (
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseDumpKeepsNoSecrets(t *testing.T) {
	out := "awg0\tSERVER_PRIVATE_KEY\tserverpub\t51820\toff\r\n" +
		"awg0\tpeerPubKeyAAAAAAAAAA=\tPRESHARED\t203.0.113.7:51000\t10.8.1.2/32\t1793791460\t1000\t2000\t0\r\n" +
		"awg0\tpeerPubKeyBBBBBBBBBB=\tPRESHARED\t(none)\t10.8.1.3/32\t0\t0\t0\t0\r\n"
	peers := parseDump(out)
	if len(peers) != 2 || peers[0].Endpoint != "203.0.113.7:51000" || peers[0].RxBytes != 1000 || peers[0].TxBytes != 2000 ||
		peers[0].LastHandshake.Unix() != 1793791460 || peers[1].Endpoint != "" || !peers[1].LastHandshake.IsZero() {
		t.Fatalf("%+v", peers)
	}
	b, _ := json.Marshal(peers)
	if strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "PRESHARED") {
		t.Fatal("keys leaked")
	}
}

func TestParseClientsTable(t *testing.T) {
	out := `[{"clientId":"peerPubKeyAAAAAAAAAA=","userData":{"clientName":"Телефон","creationDate":"Tue Nov 4 2026"}}]` + "\n" +
		`{"oldClientId":{"clientName":"Old"}}`
	got := parseClientsTable(out)
	if len(got) != 2 || got[0].Name != "Телефон" || got[0].Created == "" || got[1].ID != "oldClientId" || got[1].Name != "Old" {
		t.Fatalf("%+v", got)
	}
}

func TestDemux(t *testing.T) {
	frame := func(stream byte, s string) []byte {
		h := []byte{stream, 0, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint32(h[4:], uint32(len(s)))
		return append(h, s...)
	}
	raw := append(frame(1, "line one\n"), frame(2, "line two\n")...)
	if got := demux(raw); len(got) != 2 || got[1] != "line two" {
		t.Fatalf("%q", got)
	}
	if got := demux([]byte("plain\ntext\n")); len(got) != 2 {
		t.Fatalf("%q", got)
	}
	if got := demux(nil); len(got) != 0 {
		t.Fatalf("%q", got)
	}
}
