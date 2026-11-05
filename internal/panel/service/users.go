// Package service holds the panel's business rules on top of the store.
package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"regexp"
	"slices"
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
	AmneziaKey   string // vpn://... from the Amnezia app, or empty
	SubHidden    []int64
}

var amneziaKeyRe = regexp.MustCompile(`^vpn://[A-Za-z0-9_=+/-]+$`)

func (in UserInput) normalize() (store.UserFields, error) {
	f := store.UserFields{
		Name:         strings.TrimSpace(in.Name),
		Note:         strings.TrimSpace(in.Note),
		Enabled:      in.Enabled,
		TrafficLimit: in.TrafficLimit,
		ResetPeriod:  in.ResetPeriod,
		AmneziaKey:   strings.Join(strings.Fields(in.AmneziaKey), ""),
	}
	// An AmneziaWG config made by the panel keeps its lines.
	if conf := strings.TrimSpace(strings.ReplaceAll(in.AmneziaKey, "\r\n", "\n")); strings.HasPrefix(conf, "[Interface]") {
		if len(conf) > 8000 || !strings.Contains(conf, "PrivateKey") || !strings.Contains(conf, "Endpoint") {
			return f, validate.Fail("amneziaKey", "an AmneziaWG config needs PrivateKey and Endpoint")
		}
		f.AmneziaKey = conf + "\n"
	}
	for _, id := range in.SubHidden {
		if id > 0 && !slices.Contains(f.SubHidden, id) {
			f.SubHidden = append(f.SubHidden, id)
		}
	}
	if len(f.SubHidden) > 500 {
		return f, validate.Fail("subHidden", "too many inbounds")
	}
	if k := f.AmneziaKey; k != "" && !strings.HasPrefix(k, "[Interface]") && (len(k) < 24 || len(k) > 32000 || !amneziaKeyRe.MatchString(k)) {
		return f, validate.Fail("amneziaKey", "a key from the Amnezia app starts with vpn://")
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
	store   *store.Store
	now     func() time.Time
	changed func() // asks the syncer to push the new state to the cores
}

// NewUsers builds the service; changed may be nil.
func NewUsers(st *store.Store, changed func()) *Users {
	if changed == nil {
		changed = func() {}
	}
	return &Users{store: st, now: time.Now, changed: changed}
}

// NewUUID returns a random version-4 UUID.
func NewUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (s *Users) Now() time.Time { return s.now() }

func (s *Users) List(ctx context.Context) ([]store.User, error) { return s.store.ListUsers(ctx) }

func (s *Users) Get(ctx context.Context, id int64) (store.User, error) {
	return s.store.GetUser(ctx, id)
}

func (s *Users) Create(ctx context.Context, adminID int64, in UserInput) (store.User, error) {
	f, err := in.normalize()
	if err != nil {
		return store.User{}, err
	}
	u, err := s.store.CreateUser(ctx, f, rand.Text(), NewUUID(), s.now())
	if err != nil {
		return store.User{}, err
	}
	s.audit(ctx, adminID, "user.create", u.ID, f)
	s.changed()
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
	s.changed()
	return u, nil
}

func (s *Users) Delete(ctx context.Context, adminID, id int64) error {
	if err := s.store.DeleteUser(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, adminID, "user.delete", id, nil)
	s.changed()
	return nil
}

func (s *Users) audit(ctx context.Context, adminID int64, action string, userID int64, diff any) {
	writeAudit(ctx, s.store, s.now(), adminID, action, "user", userID, diff)
}
