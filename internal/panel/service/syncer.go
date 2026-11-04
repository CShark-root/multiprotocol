package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"panel/internal/agent"
	"panel/internal/panel/compile"
	"panel/internal/panel/store"
)

// Syncer keeps the cores in line with the database. After every change and
// once a minute it collects traffic, re-evaluates limits (expiry, quota) and
// pushes freshly compiled configs to the agent. Applying is idempotent, so
// an unchanged state costs nothing.
type Syncer struct {
	store   *store.Store
	agent   *agent.Agent
	events  *Events
	apiAddr string
	log     *slog.Logger
	now     func() time.Time
	trigger chan struct{}

	mu       sync.Mutex
	lastSync time.Time
	lastErr  string

	// What the previous sync saw, to record transitions as events. Only the
	// sync goroutine touches these.
	userStatus map[int64]Status
	coreDown   map[string]bool
}

func NewSyncer(st *store.Store, ag *agent.Agent, events *Events, xrayAPIAddr string, log *slog.Logger) *Syncer {
	return &Syncer{
		store: st, agent: ag, events: events, apiAddr: xrayAPIAddr, log: log, now: time.Now,
		trigger: make(chan struct{}, 1), coreDown: map[string]bool{},
	}
}

// Trigger schedules a sync soon; calls coalesce.
func (s *Syncer) Trigger() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

func (s *Syncer) Run(ctx context.Context) {
	s.sync(ctx)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.trigger:
			// Let a burst of edits (e.g. toggling several users) land first.
			time.Sleep(300 * time.Millisecond)
		}
		s.sync(ctx)
	}
}

// Status reports when the last sync ran and why it failed, if it did.
func (s *Syncer) Status() (time.Time, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSync, s.lastErr
}

func (s *Syncer) sync(ctx context.Context) {
	// Traffic first: counters reset when a core restarts with a new config.
	if err := s.collect(ctx); err != nil {
		s.log.Warn("collect traffic", "err", err)
	}
	err := s.apply(ctx)
	if err != nil {
		s.log.Error("apply config", "err", err)
	}
	s.watchCores(ctx)
	s.mu.Lock()
	s.lastSync = s.now()
	s.lastErr = ""
	if err != nil {
		s.lastErr = err.Error()
	}
	s.mu.Unlock()
}

// recordUserChanges writes an event when a user runs out of traffic or time,
// or gets access back. Admin toggles are in the audit log, not here.
func (s *Syncer) recordUserChanges(ctx context.Context, users []store.User, now map[int64]Status) {
	prev := s.userStatus
	s.userStatus = now
	if prev == nil {
		return // first sync after start: nothing to compare with
	}
	for _, u := range users {
		was, ok := prev[u.ID]
		cur := now[u.ID]
		if !ok || was == cur {
			continue
		}
		data := map[string]any{"name": u.Name}
		switch {
		case cur == StatusLimited:
			s.events.Add(ctx, EventUserLimited, "warning", u.ID, "", data)
		case cur == StatusExpired:
			s.events.Add(ctx, EventUserExpired, "warning", u.ID, "", data)
		case cur == StatusActive && (was == StatusLimited || was == StatusExpired):
			s.events.Add(ctx, EventUserRestored, "info", u.ID, "", data)
		}
	}
}

// watchCores records a core going down (not running, or reporting an error)
// and coming back.
func (s *Syncer) watchCores(ctx context.Context) {
	for _, c := range s.agent.Health(ctx).Cores {
		if !c.Installed {
			continue
		}
		down := c.Message != ""
		if down && !s.coreDown[c.Name] {
			s.events.Add(ctx, EventCoreDown, "danger", 0, c.Message, map[string]any{"core": c.Name})
		}
		if !down && s.coreDown[c.Name] && c.Running {
			s.events.Add(ctx, EventCoreUp, "info", 0, "", map[string]any{"core": c.Name, "version": c.Version})
		}
		s.coreDown[c.Name] = down
	}
}

func (s *Syncer) collect(ctx context.Context) error {
	deltas, err := s.agent.Traffic(ctx)
	byUser := map[int64]int64{}
	for _, d := range deltas {
		if id, ok := compile.UserIDFromEmail(d.Email); ok {
			byUser[id] += d.Up + d.Down
		}
	}
	if len(byUser) > 0 {
		if err := s.store.AddTraffic(ctx, byUser, s.now()); err != nil {
			return err
		}
	}
	return err
}

func (s *Syncer) apply(ctx context.Context) error {
	inbounds, err := s.store.ListInbounds(ctx)
	if err != nil {
		return err
	}
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	active := users[:0:0]
	statuses := make(map[int64]Status, len(users))
	for _, u := range users {
		st := StatusOf(u, now)
		statuses[u.ID] = st
		if st == StatusActive {
			active = append(active, u)
		}
	}
	s.recordUserChanges(ctx, users, statuses)
	xrayCfg, err := compile.Xray(inbounds, active, s.apiAddr)
	if err != nil {
		return err
	}
	hyCfg, err := compile.Hysteria(inbounds, active)
	if err != nil {
		return err
	}
	sbCfg, err := compile.SingBox(inbounds, active)
	if err != nil {
		return err
	}
	return s.agent.Apply(ctx, agent.NodeState{Configs: map[string][]byte{
		"xray": xrayCfg, "hysteria": hyCfg, "singbox": sbCfg,
	}})
}
