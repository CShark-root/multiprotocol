package journal

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/panel/store"
)

func TestHandlerCapturesAndPersists(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hub := NewHub(st)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { hub.Run(ctx); close(done) }()

	live, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	log := slog.New(hub.Handler(slog.NewTextHandler(io.Discard, nil)))
	log.Debug("too chatty to keep")
	log.Info("panel started", "listen", ":443")
	log.With("source", "xray").Info("\x1b[33m2026/11/04 [Warning] core: something odd\x1b[0m")
	log.With("source", "sing-box").Info("ERROR inbound/tuic: handshake failed")

	var got []store.LogEntry
	for len(got) < 3 {
		select {
		case e := <-live:
			got = append(got, e)
		case <-time.After(2 * time.Second):
			t.Fatalf("live entries: %+v", got)
		}
	}
	if got[0].Source != "panel" || got[0].Fields["listen"] != ":443" {
		t.Errorf("panel entry: %+v", got[0])
	}
	if got[1].Source != "xray" || got[1].Level != "warn" || got[1].Msg[0] == '\x1b' {
		t.Errorf("xray entry (level from text, ANSI stripped): %+v", got[1])
	}
	if got[2].Level != "error" {
		t.Errorf("sing-box entry: %+v", got[2])
	}

	cancel()
	<-done // flushes the batch
	stored, err := st.QueryLogs(context.Background(), store.LogFilter{MinLevel: "warn"})
	if err != nil || len(stored) != 2 {
		t.Fatalf("warn+ entries in the database: %d %v", len(stored), err)
	}
	if found, _ := st.QueryLogs(context.Background(), store.LogFilter{Query: "handshake"}); len(found) != 1 {
		t.Fatalf("text search: %+v", found)
	}
	if found, _ := st.QueryLogs(context.Background(), store.LogFilter{Query: "100%_"}); len(found) != 0 {
		t.Fatal("LIKE wildcards in the query must be literal")
	}
}
