package xray

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestParseStats(t *testing.T) {
	out := []byte(`{"stat":[
		{"name":"user>>>u1>>>traffic>>>uplink","value":"100"},
		{"name":"user>>>u1>>>traffic>>>downlink","value":2500},
		{"name":"user>>>u2>>>traffic>>>downlink"},
		{"name":"inbound>>>in-1>>>traffic>>>uplink","value":9}
	]}`)
	d, err := parseStats(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 1 || d[0].Email != "u1" || d[0].Up != 100 || d[0].Down != 2500 {
		t.Fatalf("unexpected deltas: %+v", d)
	}
	if _, err := parseStats([]byte("not json")); err == nil {
		t.Fatal("garbage must be an error")
	}
}

func TestApplyWithoutBinary(t *testing.T) {
	dir := t.TempDir()
	a := New(filepath.Join(dir, "missing-xray"), dir, "127.0.0.1:1", slog.Default())
	if err := a.Apply(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected an error without the binary")
	}
	h := a.Health(context.Background())
	if h.Installed || h.Running || h.Message == "" {
		t.Fatalf("unexpected health: %+v", h)
	}
	if err := a.Apply(context.Background(), nil); err != nil {
		t.Fatalf("stopping must always work: %v", err)
	}
}

func TestParseOnline(t *testing.T) {
	out := []byte(`{"users":[{"email":"u3","ips":[{"ip":"192.168.1.150","lastSeen":1793791460},{"ip":"10.0.0.2","lastSeen":1}],"traffic":{"downlink":4011570,"uplink":723}},{"email":"u9","ips":[]}]}`)
	got, err := parseOnline(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Email != "u3" || got[0].Connections != 2 || len(got[0].IPs) != 2 || got[0].IPs[0] != "192.168.1.150" {
		t.Fatalf("got %+v", got)
	}
	if got, err := parseOnline([]byte("{}")); err != nil || len(got) != 0 {
		t.Fatalf("empty: %+v %v", got, err)
	}
}
