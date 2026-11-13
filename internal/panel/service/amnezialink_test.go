package service

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/amnezia"
	"panel/internal/panel/store"
)

// fakeAmnezia считает вызовы SetPaused: регрессионный тест на панику
// «assignment to entry in nil map» и на пропуск неизменных кругов.
type fakeAmnezia struct {
	calls  int
	params []amnezia.PauseParams
}

func (f *fakeAmnezia) Status(context.Context) (amnezia.Status, error) { return amnezia.Status{}, nil }

func (f *fakeAmnezia) SetPaused(_ context.Context, p amnezia.PauseParams) (amnezia.PauseResult, error) {
	f.calls++
	f.params = append(f.params, p)
	return amnezia.PauseResult{}, nil
}

func TestApplyAmneziaSkipsUnchanged(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	fake := &fakeAmnezia{}
	s := &Syncer{
		store:   st,
		events:  NewEvents(st),
		amnezia: fake,
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	started := time.Now()
	s.amneziaSeen = &amnezia.Status{Containers: []amnezia.Container{
		{Name: "amnezia-awg2", Kind: "awg2", Managed: true, State: "running", StartedAt: started},
	}}

	s.applyAmnezia(ctx, map[int64]Status{}) // первый круг: записывает подпись
	s.applyAmnezia(ctx, map[int64]Status{}) // ничего не изменилось: вызова быть не должно
	if fake.calls != 1 {
		t.Fatalf("SetPaused calls: got %d, want 1 (unchanged rounds must be skipped)", fake.calls)
	}

	s.amneziaSeen.Containers[0].StartedAt = started.Add(time.Minute) // контейнер перезапустился
	s.applyAmnezia(ctx, map[int64]Status{})
	if fake.calls != 2 {
		t.Fatalf("SetPaused calls after container restart: got %d, want 2", fake.calls)
	}
}
