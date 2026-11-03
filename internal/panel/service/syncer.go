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
	apiAddr string
	log     *slog.Logger
	now     func() time.Time
	trigger chan struct{}

	mu       sync.Mutex
	lastSync time.Time
	lastErr  string
}

func NewSyncer(st *store.Store, ag *agent.Agent, xrayAPIAddr string, log *slog.Logger) *Syncer {
	return &Syncer{
		store: st, agent: ag, apiAddr: xrayAPIAddr, log: log, now: time.Now,
		trigger: make(chan struct{}, 1),
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
	s.mu.Lock()
	s.lastSync = s.now()
	s.lastErr = ""
	if err != nil {
		s.lastErr = err.Error()
	}
	s.mu.Unlock()
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
	active := users[:0]
	for _, u := range users {
		if StatusOf(u, now) == StatusActive {
			active = append(active, u)
		}
	}
	cfg, err := compile.Xray(inbounds, active, s.apiAddr)
	if err != nil {
		return err
	}
	return s.agent.Apply(ctx, agent.NodeState{Configs: map[string][]byte{"xray": cfg}})
}
