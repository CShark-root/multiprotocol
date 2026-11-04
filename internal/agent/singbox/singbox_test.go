package singbox

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
)

func TestAccountAttributesByUserOutbound(t *testing.T) {
	a := New(filepath.Join(t.TempDir(), "missing"), t.TempDir(), slog.Default())
	snap := func(up, down int64) connections {
		return connections{Connections: []connEntry{
			{ID: "c1", Upload: up, Download: down, Chains: []string{"direct-u7"}},
			{ID: "c2", Upload: 999, Download: 999, Chains: []string{"direct"}}, // not a user outbound
		}}
	}
	a.account(snap(100, 1000))
	a.account(snap(150, 5000)) // cumulative: only the growth counts
	d, _ := a.Traffic(context.Background())
	if len(d) != 1 || d[0].Email != "u7" || d[0].Up != 150 || d[0].Down != 5000 {
		t.Fatalf("unexpected deltas: %+v", d)
	}
	if d, _ := a.Traffic(context.Background()); len(d) != 0 {
		t.Fatalf("traffic must reset after reading: %+v", d)
	}
	a.account(connections{}) // connection closed
	a.account(snap(10, 20))  // a new connection reusing the id starts from zero
	if d, _ := a.Traffic(context.Background()); len(d) != 1 || d[0].Up != 10 || d[0].Down != 20 {
		t.Fatalf("new connection: %+v", d)
	}
}

func TestApplyWithoutBinary(t *testing.T) {
	a := New(filepath.Join(t.TempDir(), "missing"), t.TempDir(), slog.Default())
	if err := a.Apply(context.Background(), []byte(`{"inbounds":[]}`)); err == nil {
		t.Fatal("expected an error without the binary")
	}
	if h := a.Health(context.Background()); h.Installed || h.Message == "" {
		t.Fatalf("unexpected health: %+v", h)
	}
	if err := a.Apply(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}
