package service

import (
	"testing"
	"time"
)

func TestDaysLeft(t *testing.T) {
	now := time.Date(2026, 11, 28, 15, 0, 0, 0, time.Local)
	for date, want := range map[string]int{"2026-12-01": 3, "2026-11-29": 1, "2026-11-28": 0, "2026-11-27": -1} {
		p, _, err := ParseBilling(date, "")
		if err != nil || daysLeft(*p, now) != want {
			t.Errorf("%s: got %d, want %d (%v)", date, daysLeft(*p, now), want, err)
		}
	}
	if got := billingText(map[string]any{"server": "NL", "date": "01.12.2026", "days": 3, "price": "5 €"}); got != "💳 «NL»: оплата через 3 дня (до 01.12.2026, 5 €)" {
		t.Errorf("text: %q", got)
	}
}
