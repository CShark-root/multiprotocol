// Package journal collects the panel's own log records and the output of
// the cores into one place: it persists them in batches (pruned to a few
// days) and fans new entries out to live viewers.
package journal

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"panel/internal/panel/store"
)

const (
	keepFor     = 7 * 24 * time.Hour
	keepRows    = 200_000
	eventsKeep  = 90 * 24 * time.Hour
	trafficDays = 400 // daily per-user traffic history
	maxMsgBytes = 2000
)

type Hub struct {
	store *store.Store
	in    chan store.LogEntry

	mu   sync.Mutex
	subs map[chan store.LogEntry]struct{}
}

func NewHub(st *store.Store) *Hub {
	return &Hub{store: st, in: make(chan store.LogEntry, 4096), subs: map[chan store.LogEntry]struct{}{}}
}

// Run writes entries to the database in batches and prunes old ones.
func (h *Hub) Run(ctx context.Context) {
	flush := time.NewTicker(time.Second)
	prune := time.NewTicker(time.Hour)
	defer flush.Stop()
	defer prune.Stop()
	var batch []store.LogEntry
	write := func() {
		if len(batch) == 0 {
			return
		}
		// Best effort: a failed write must not block or recurse into logging.
		h.store.InsertLogs(context.Background(), batch)
		batch = batch[:0]
	}
	h.prune()
	for {
		select {
		case <-ctx.Done():
			// Drain what is still queued so the last lines before a
			// shutdown are not lost.
			for {
				select {
				case e := <-h.in:
					batch = append(batch, e)
					continue
				default:
				}
				break
			}
			write()
			return
		case e := <-h.in:
			batch = append(batch, e)
			if len(batch) >= 500 {
				write()
			}
		case <-flush.C:
			write()
		case <-prune.C:
			h.prune()
		}
	}
}

func (h *Hub) prune() {
	now := time.Now()
	h.store.PruneLogs(context.Background(), keepFor, keepRows, now)
	h.store.PruneEvents(context.Background(), eventsKeep, now)
	h.store.PruneTrafficDaily(context.Background(), now.Local().AddDate(0, 0, -trafficDays).Format(time.DateOnly))
}

// Subscribe returns new entries as they arrive. Slow readers miss entries
// rather than slowing the panel down.
func (h *Hub) Subscribe() (<-chan store.LogEntry, func()) {
	ch := make(chan store.LogEntry, 256)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

func (h *Hub) publish(e store.LogEntry) {
	select {
	case h.in <- e:
	default: // database writer is behind; drop rather than block
	}
	h.mu.Lock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
		}
	}
	h.mu.Unlock()
}

// Handler wraps next so that every record at Info or above also goes to
// the hub. The "source" attribute (set by core adapters) names the origin.
func (h *Hub) Handler(next slog.Handler) slog.Handler { return &handler{next: next, hub: h} }

type handler struct {
	next   slog.Handler
	hub    *Hub
	attrs  []slog.Attr
	prefix string
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool { return h.next.Enabled(ctx, l) }

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelInfo {
		e := store.LogEntry{TS: r.Time, Level: levelName(r.Level), Source: "panel", Fields: map[string]string{}}
		add := func(a slog.Attr) {
			if a.Key == "source" {
				e.Source = a.Value.String()
				return
			}
			e.Fields[h.prefix+a.Key] = a.Value.String()
		}
		for _, a := range h.attrs {
			add(a)
		}
		r.Attrs(func(a slog.Attr) bool { add(a); return true })
		e.Msg = clean(r.Message)
		if e.Source != "panel" {
			// Core output arrives at Info; recover the level the core printed.
			e.Level = coreLevel(e.Msg, e.Level)
		}
		if len(e.Fields) == 0 {
			e.Fields = nil
		}
		h.hub.publish(e)
	}
	return h.next.Handle(ctx, r)
}

func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	attrs := append(append([]slog.Attr{}, h.attrs...), as...)
	return &handler{next: h.next.WithAttrs(as), hub: h.hub, attrs: attrs, prefix: h.prefix}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{next: h.next.WithGroup(name), hub: h.hub, attrs: h.attrs, prefix: h.prefix + name + "."}
}

func levelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warn"
	case l >= slog.LevelInfo:
		return "info"
	}
	return "debug"
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func clean(s string) string {
	s = ansi.ReplaceAllString(s, "")
	if len(s) > maxMsgBytes {
		s = s[:maxMsgBytes] + "…"
	}
	return s
}

// coreLevel recognises the level words Xray ("[Warning]"), Hysteria and
// sing-box ("WARN", "ERROR", "FATAL") put into their lines.
func coreLevel(line, fallback string) string {
	head := line
	if len(head) > 80 {
		head = head[:80]
	}
	u := strings.ToUpper(head)
	switch {
	case strings.Contains(u, "ERROR") || strings.Contains(u, "FATAL") || strings.Contains(u, "PANIC"):
		return "error"
	case strings.Contains(u, "WARN"):
		return "warn"
	}
	return fallback
}
