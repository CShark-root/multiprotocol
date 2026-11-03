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
