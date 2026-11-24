package service

import (
	"strings"
	"testing"
	"time"

	"panel/internal/panel/store"
)

func TestQuotaStatusAndNotification(t *testing.T) {
	now := time.Now()
	u := store.User{Enabled: true, TrafficLimit: 100, TrafficUsed: 100, QuotaAction: "throttle", ThrottledSpeedMbps: 5}
	if got := StatusOf(u, now); got != StatusThrottled {
		t.Fatalf("status %q, want throttled", got)
	}
	e := store.Event{Type: EventUserThrottled, Data: map[string]any{"speedMbps": 5}}
	if text := eventText(e, "phone"); !strings.Contains(text, "скорость ограничена до 5") || strings.Contains(text, "приостановлен") {
		t.Fatalf("wrong throttle notification: %s", text)
	}
	u.QuotaAction = "block"
	if got := StatusOf(u, now); got != StatusLimited {
		t.Fatalf("status %q, want limited", got)
	}
}

func TestUserButtonMarks(t *testing.T) {
	now := time.Now()
	u := store.User{Name: "phone", Enabled: true}
	if got := userButton(u, now, 0); !strings.HasPrefix(got, "⚪") {
		t.Fatalf("offline: %s", got)
	}
	if got := userButton(u, now, 1); !strings.HasPrefix(got, "🟢") {
		t.Fatalf("online: %s", got)
	}
	u.TrafficLimit, u.TrafficUsed, u.QuotaAction = 100, 100, "throttle"
	if got := userButton(u, now, 1); !strings.HasPrefix(got, "🟡") {
		t.Fatalf("throttled: %s", got)
	}
	u.QuotaAction = "block"
	if got := userButton(u, now, 1); !strings.HasPrefix(got, "🔴") {
		t.Fatalf("stopped: %s", got)
	}
}

func TestUserRates(t *testing.T) {
	now := time.Now()
	s := &Syncer{lastSampleAt: now.Add(-5 * time.Second), rates: map[int64]UserRate{}, now: func() time.Time { return now }}
	s.updateRates(map[int64]store.TrafficAmount{1: {Up: 50, Down: 200}}, now)
	rate := s.Rates()[1]
	if rate.UpBps != 10 || rate.DownBps != 40 {
		t.Fatalf("rate %+v", rate)
	}
	now = now.Add(5 * time.Second)
	s.updateRates(map[int64]store.TrafficAmount{}, now)
	if len(s.Rates()) != 0 {
		t.Fatal("idle users must not retain an old nonzero rate")
	}
	s.updateRates(map[int64]store.TrafficAmount{1: {Up: 50, Down: 200}}, now)
	now = now.Add(16 * time.Second)
	if len(s.Rates()) != 0 {
		t.Fatal("a stale rate must expire after repeated collection failures")
	}
}
