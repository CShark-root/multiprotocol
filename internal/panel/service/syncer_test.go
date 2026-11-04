package service

import (
	"context"
	"path/filepath"
	"testing"

	"panel/internal/panel/store"
)

func TestRecordUserChanges(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	s := &Syncer{store: st, events: NewEvents(st)}
	u := store.User{ID: 1, Name: "phone"}
	types := func() []string {
		ev, _ := st.ListEvents(ctx, 0, 10)
		var out []string
		for _, e := range ev {
			out = append(out, e.Type)
		}
		return out
	}

	s.recordUserChanges(ctx, []store.User{u}, map[int64]Status{1: StatusLimited})
	if got := types(); len(got) != 0 {
		t.Fatalf("the first sync only learns the state, got %v", got)
	}
	s.recordUserChanges(ctx, []store.User{u}, map[int64]Status{1: StatusLimited})
	s.recordUserChanges(ctx, []store.User{u}, map[int64]Status{1: StatusActive})
	s.recordUserChanges(ctx, []store.User{u}, map[int64]Status{1: StatusExpired})
	s.recordUserChanges(ctx, []store.User{u}, map[int64]Status{1: StatusDisabled}) // admin action: audit, not an event
	got := types()
	want := []string{EventUserExpired, EventUserRestored} // newest first
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("events %v, want %v", got, want)
	}
}
