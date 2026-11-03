// Package service holds the panel's business rules on top of the store.
package service

import (
	"context"
	"crypto/rand"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"panel/internal/panel/store"
	"panel/internal/panel/validate"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
	StatusExpired  Status = "expired"
	StatusLimited  Status = "limited"
)

// StatusOf derives the effective status. Only "enabled" is stored; the rest
// follows from the limits, so it can never go stale.
func StatusOf(u store.User, now time.Time) Status {
	switch {
	case !u.Enabled:
		return StatusDisabled
	case u.ExpireAt != nil && !now.Before(*u.ExpireAt):
		return StatusExpired
	case u.TrafficLimit > 0 && u.TrafficUsed >= u.TrafficLimit:
		return StatusLimited
	default:
		return StatusActive
	}
}

var ResetPeriods = []string{"none", "day", "week", "month"}

// Names end up as the user's "email" tag inside Xray configs, so keep them
// to a safe character set.
var userNameRe = regexp.MustCompile(`^[\p{L}\p{N}_.-]{1,64}$`)

type UserInput struct {
	Name         string
	Note         string
	Enabled      bool
	TrafficLimit int64
	ResetPeriod  string
	ExpireAt     *time.Time
}

func (in UserInput) normalize() (store.UserFields, error) {
	f := store.UserFields{
		Name:         strings.TrimSpace(in.Name),
		Note:         strings.TrimSpace(in.Note),
		Enabled:      in.Enabled,
		TrafficLimit: in.TrafficLimit,
		ResetPeriod:  in.ResetPeriod,
	}
	if f.ResetPeriod == "" {
		f.ResetPeriod = "none"
	}
	if !userNameRe.MatchString(f.Name) {
		return f, validate.Fail("name", "1-64 letters, digits, '_', '.' or '-'")
	}
	if utf8.RuneCountInString(f.Note) > 500 {
		return f, validate.Fail("note", "at most 500 characters")
	}
	if f.TrafficLimit < 0 {
		return f, validate.Fail("trafficLimit", "must be 0 (unlimited) or positive")
	}
	if !slices.Contains(ResetPeriods, f.ResetPeriod) {
		return f, validate.Fail("resetPeriod", "one of "+strings.Join(ResetPeriods, ", "))
	}
	if in.ExpireAt != nil {
		t := in.ExpireAt.UTC().Truncate(time.Second)
		f.ExpireAt = &t
	}
	return f, nil
}

type Users struct {
	store *store.Store
	now   func() time.Time
}

func NewUsers(st *store.Store) *Users {
	return &Users{store: st, now: time.Now}
}

func (s *Users) Now() time.Time { return s.now() }

func (s *Users) List(ctx context.Context) ([]store.User, error) { return s.store.ListUsers(ctx) }

func (s *Users) Get(ctx context.Context, id int64) (store.User, error) { return s.store.GetUser(ctx, id) }

func (s *Users) Create(ctx context.Context, adminID int64, in UserInput) (store.User, error) {
	f, err := in.normalize()
	if err != nil {
		return store.User{}, err
	}
	u, err := s.store.CreateUser(ctx, f, rand.Text(), s.now())
	if err != nil {
		return store.User{}, err
	}
	s.audit(ctx, adminID, "user.create", u.ID, f)
	return u, nil
}

func (s *Users) Update(ctx context.Context, adminID, id int64, in UserInput) (store.User, error) {
	f, err := in.normalize()
	if err != nil {
		return store.User{}, err
	}
	u, err := s.store.UpdateUser(ctx, id, f, s.now())
	if err != nil {
		return store.User{}, err
	}
	s.audit(ctx, adminID, "user.update", id, f)
	return u, nil
}

func (s *Users) Delete(ctx context.Context, adminID, id int64) error {
	if err := s.store.DeleteUser(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, adminID, "user.delete", id, nil)
	return nil
}

// audit records the change; a failure here must not undo or hide a change
// that already happened, so it is only logged.
func (s *Users) audit(ctx context.Context, adminID int64, action string, userID int64, diff any) {
	err := s.store.AddAudit(ctx, store.AuditEntry{
		AdminID:    adminID,
		Action:     action,
		TargetType: "user",
		TargetID:   strconv.FormatInt(userID, 10),
		Diff:       diff,
	}, s.now())
	if err != nil {
		slog.Warn("audit log write failed", "action", action, "err", err)
	}
}
