package service

import (
	"context"
	"time"

	"panel/internal/panel/store"
	"panel/internal/panel/validate"
)

const EventUserReset = "user.reset"

// PeriodStart is the beginning of the reset period that contains now, in the
// server's local time: midnight, Monday midnight or the 1st of the month.
func PeriodStart(now time.Time, period string) (time.Time, bool) {
	now = now.Local()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch period {
	case "day":
		return day, true
	case "week":
		offset := (int(day.Weekday()) + 6) % 7 // Monday = 0
		return day.AddDate(0, 0, -offset), true
	case "month":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()), true
	}
	return time.Time{}, false
}

// applyResets zeroes the counters of users whose reset period has rolled
// over since their last reset, updating users in place.
func (s *Syncer) applyResets(ctx context.Context, users []store.User, now time.Time) {
	for i := range users {
		u := &users[i]
		start, ok := PeriodStart(now, u.ResetPeriod)
		if !ok || !u.LastResetAt.Before(start) {
			continue
		}
		if err := s.store.ResetTraffic(ctx, u.ID, now); err != nil {
			s.log.Warn("periodic traffic reset", "user", u.ID, "err", err)
			continue
		}
		if u.TrafficUsed > 0 {
			s.events.Add(ctx, EventUserReset, "info", u.ID, "", map[string]any{"name": u.Name, "bytes": u.TrafficUsed})
		}
		u.TrafficUsed, u.LastResetAt = 0, now
	}
}

// ResetTraffic zeroes a user's counter by hand.
func (s *Users) ResetTraffic(ctx context.Context, adminID, id int64) (store.User, error) {
	if err := s.store.ResetTraffic(ctx, id, s.now()); err != nil {
		return store.User{}, err
	}
	s.audit(ctx, adminID, "user.reset_traffic", id, nil)
	s.changed()
	return s.store.GetUser(ctx, id)
}

// Extend adds days to a user's access, counting from now when it has
// already expired. A user without expiry stays unlimited.
func (s *Users) Extend(ctx context.Context, adminID, id int64, days int) (store.User, error) {
	if days < 1 || days > 3660 {
		return store.User{}, validate.Fail("days", "1-3660")
	}
	u, err := s.store.GetUser(ctx, id)
	if err != nil {
		return store.User{}, err
	}
	if u.ExpireAt == nil {
		return u, nil
	}
	now := s.now()
	from := *u.ExpireAt
	if from.Before(now) {
		from = now
	}
	next := from.AddDate(0, 0, days).UTC().Truncate(time.Second)
	if err := s.store.SetExpire(ctx, id, &next, now); err != nil {
		return store.User{}, err
	}
	s.audit(ctx, adminID, "user.extend", id, map[string]any{"days": days, "expireAt": next})
	s.changed()
	return s.store.GetUser(ctx, id)
}
