package service

import (
	"context"
	"log/slog"
	"time"

	"panel/internal/panel/store"
)

// Event types. The UI turns them into readable, localized text; Message is
// an extra technical detail.
const (
	EventUserLimited   = "user.limited"
	EventUserExpired   = "user.expired"
	EventUserRestored  = "user.restored"
	EventCoreDown      = "core.down"
	EventCoreUp        = "core.up"
	EventLogin         = "auth.login"
	EventLoginFailed   = "auth.failed"
	EventLoginThrottle = "auth.ratelimited"
)

type Events struct {
	store     *store.Store
	now       func() time.Time
	listeners []func(store.Event)
}

// Listen registers fn for every new event (Telegram notifications). It runs
// in its own goroutine, so a slow listener never delays the action.
func (e *Events) Listen(fn func(store.Event)) { e.listeners = append(e.listeners, fn) }

func NewEvents(st *store.Store) *Events { return &Events{store: st, now: time.Now} }

// Add records an event. Failures are logged only: an event must never break
// the action it describes.
func (e *Events) Add(ctx context.Context, typ, severity string, userID int64, message string, data map[string]any) {
	if e == nil {
		return
	}
	ev := store.Event{TS: e.now(), Type: typ, Severity: severity, UserID: userID, Message: message, Data: data}
	err := e.store.AddEvent(ctx, ev)
	for _, fn := range e.listeners {
		go fn(ev)
	}
	if err != nil {
		slog.Warn("event write failed", "type", typ, "err", err)
	}
}
