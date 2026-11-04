package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestTrafficDaily(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.Local)
	a, _ := st.CreateUser(ctx, UserFields{Name: "a", Enabled: true}, "tokA", "uuid-a", now)
	b, _ := st.CreateUser(ctx, UserFields{Name: "b", Enabled: true}, "tokB", "uuid-b", now)

	yesterday := now.AddDate(0, 0, -1)
	must(t, st.AddTraffic(ctx, map[int64]int64{a.ID: 100}, yesterday))
	must(t, st.AddTraffic(ctx, map[int64]int64{a.ID: 10, b.ID: 5}, now))
	must(t, st.AddTraffic(ctx, map[int64]int64{a.ID: 1}, now))

	all, _ := st.TrafficByDay(ctx, 0, "2026-11-01")
	if len(all) != 2 || all[0] != (DayTraffic{"2026-11-03", 100}) || all[1] != (DayTraffic{"2026-11-04", 16}) {
		t.Fatalf("all: %+v", all)
	}
	onlyA, _ := st.TrafficByDay(ctx, a.ID, "2026-11-04")
	if len(onlyA) != 1 || onlyA[0].Bytes != 11 {
		t.Fatalf("a: %+v", onlyA)
	}
	u, _ := st.GetUser(ctx, a.ID)
	if u.TrafficUsed != 111 {
		t.Fatalf("used %d", u.TrafficUsed)
	}

	// A reset zeroes the counter but keeps the history.
	must(t, st.ResetTraffic(ctx, a.ID, now))
	u, _ = st.GetUser(ctx, a.ID)
	if after, _ := st.TrafficByDay(ctx, a.ID, "2026-11-01"); u.TrafficUsed != 0 || len(after) != 2 {
		t.Fatalf("after reset: %d %+v", u.TrafficUsed, after)
	}

	// Deleting b moves its history into the server total: a new user that
	// gets b's id must start clean, and the total must not shrink.
	must(t, st.DeleteUser(ctx, b.ID))
	if all, _ := st.TrafficByDay(ctx, 0, "2026-11-01"); all[1].Bytes != 16 {
		t.Fatalf("total after delete: %+v", all)
	}
	c, _ := st.CreateUser(ctx, UserFields{Name: "c", Enabled: true}, "tokC", "uuid-c", now)
	if got, _ := st.TrafficByDay(ctx, c.ID, "2000-01-01"); c.ID != b.ID || len(got) != 0 {
		t.Fatalf("reused id %d (b was %d) inherited %+v", c.ID, b.ID, got)
	}

	must(t, st.PruneTrafficDaily(ctx, "2026-11-04"))
	if left, _ := st.TrafficByDay(ctx, 0, "2000-01-01"); len(left) != 1 {
		t.Fatalf("after prune: %+v", left)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
