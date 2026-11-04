package service

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/panel/store"
)

func TestPeriodStart(t *testing.T) {
	// Wednesday 2026-11-04 15:30 local time.
	now := time.Date(2026, 11, 4, 15, 30, 0, 0, time.Local)
	for period, want := range map[string]time.Time{
		"day":   time.Date(2026, 11, 4, 0, 0, 0, 0, time.Local),
		"week":  time.Date(2026, 11, 2, 0, 0, 0, 0, time.Local), // Monday
		"month": time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local),
	} {
		got, ok := PeriodStart(now, period)
		if !ok || !got.Equal(want) {
			t.Errorf("%s: got %v, want %v", period, got, want)
		}
	}
	if _, ok := PeriodStart(now, "none"); ok {
		t.Error("none has no period")
	}
	sunday := time.Date(2026, 11, 8, 23, 0, 0, 0, time.Local)
	if got, _ := PeriodStart(sunday, "week"); got.Day() != 2 {
		t.Errorf("Sunday belongs to the week starting Monday the 2nd, got %v", got)
	}
}

func TestApplyResetsAndExtend(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	created := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	u, _ := st.CreateUser(ctx, store.UserFields{Name: "m", Enabled: true, ResetPeriod: "month"}, "TOKEN1", NewUUID(), created)
	st.AddTraffic(ctx, map[int64]int64{u.ID: 5000}, created)

	s := &Syncer{store: st, events: NewEvents(st), log: slog.Default()}
	users, _ := st.ListUsers(ctx)
	s.applyResets(ctx, users, created.Add(time.Hour)) // same month: nothing
	if users[0].TrafficUsed != 5000 {
		t.Fatalf("reset too early: %+v", users[0])
	}
	s.applyResets(ctx, users, time.Date(2026, 11, 1, 0, 5, 0, 0, time.Local))
	got, _ := st.GetUser(ctx, u.ID)
	if got.TrafficUsed != 0 || users[0].TrafficUsed != 0 {
		t.Fatalf("monthly reset did not happen: %+v", got)
	}
	if ev, _ := st.ListEvents(ctx, 0, 5); len(ev) != 1 || ev[0].Type != EventUserReset {
		t.Fatalf("reset event: %+v", ev)
	}
	if days, _ := st.TrafficByDay(ctx, 0, "2026-10-01"); len(days) != 1 || days[0].Bytes != 5000 {
		t.Fatalf("daily history must survive a reset: %+v", days)
	}

	// Extend counts from now when access has already expired.
	users2 := NewUsers(st, nil)
	past := time.Now().AddDate(0, 0, -10)
	st.SetExpire(ctx, u.ID, &past, time.Now())
	ext, err := users2.Extend(ctx, 0, u.ID, 30)
	if err != nil || ext.ExpireAt == nil || ext.ExpireAt.Before(time.Now().AddDate(0, 0, 29)) {
		t.Fatalf("extend from now: %+v %v", ext.ExpireAt, err)
	}
	ext2, _ := users2.Extend(ctx, 0, u.ID, 30)
	if d := ext2.ExpireAt.Sub(*ext.ExpireAt); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Fatalf("extend an active user adds to the current expiry: %v", d)
	}
}
