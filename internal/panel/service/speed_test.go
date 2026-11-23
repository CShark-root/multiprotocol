package service

import (
	"testing"

	"panel/internal/panel/store"
)

func TestHysteriaUsersAfterQuota(t *testing.T) {
	users := []store.User{
		{ID: 1, TrafficLimit: 100, TrafficUsed: 100, QuotaAction: "throttle", ThrottledSpeedMbps: 5},
		{ID: 2, TrafficLimit: 100, TrafficUsed: 100, QuotaAction: "throttle", ThrottledSpeedMbps: 5},
		{ID: 3, TrafficLimit: 100, TrafficUsed: 20, QuotaAction: "throttle", ThrottledSpeedMbps: 5},
	}
	got := hysteriaUsers(users)
	if len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("Hysteria users after quota: %+v", got)
	}
}

func TestSpeedInputValidation(t *testing.T) {
	in := UserInput{Name: "limited", Enabled: true, SpeedLimitMbps: 5, QuotaAction: "throttle", ThrottledSpeedMbps: 1}
	f, err := in.normalize()
	if err != nil || f.SpeedLimitMbps != 5 || f.ThrottledSpeedMbps != 1 {
		t.Fatalf("valid speed fields: %+v, %v", f, err)
	}
	in.ThrottledSpeedMbps = 0
	if _, err := in.normalize(); err == nil {
		t.Fatal("throttle without a speed must be rejected")
	}
	in.ThrottledSpeedMbps = 1
	in.QuotaAction = "unknown"
	if _, err := in.normalize(); err == nil {
		t.Fatal("unknown quota action must be rejected")
	}
}
