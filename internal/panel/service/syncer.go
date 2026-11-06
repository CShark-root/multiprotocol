package service

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/agent"
	"panel/internal/amnezia"
	"panel/internal/nethelper"
	"panel/internal/panel/compile"
	"panel/internal/panel/routing"
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
	routing *Routing // nil: defaults (tests)
	hops    HopSetter
	hopErr  string // last port hopping error, to log each change once
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

	amnezia     AmneziaClients  // nil: без связки с Amnezia
	amneziaSeen *amnezia.Status // что помощник показал в этом круге
	nodes       *Nodes          // nil: только этот сервер
	mainHost    string          // адрес главного сервера для каскадов
}

func NewSyncer(st *store.Store, ag *agent.Agent, events *Events, xrayAPIAddr string, log *slog.Logger) *Syncer {
	return &Syncer{
		store: st, agent: ag, events: events, apiAddr: xrayAPIAddr, log: log, now: time.Now,
		trigger: make(chan struct{}, 1), coreDown: map[string]bool{},
	}
}

// HopSetter applies UDP port hopping ranges (the root network helper).
type HopSetter interface {
	SetHops(ctx context.Context, hops []nethelper.Hop) error
}

// SetHops gives the syncer the network helper for Hysteria port hopping.
func (s *Syncer) SetHops(h HopSetter) { s.hops = h }

// SetRouting gives the syncer the routing settings to compile with.
func (s *Syncer) SetRouting(r *Routing) { s.routing = r }

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
	if err := s.collectAmnezia(ctx); err != nil {
		s.log.Warn("collect amnezia traffic", "err", err)
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
	if s.nodes != nil {
		deltas = append(deltas, s.nodes.TakeTraffic()...)
	}
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
	s.applyResets(ctx, users, now)
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
	s.applyAmnezia(ctx, statuses)
	rt := compile.DefaultRouting()
	if s.routing != nil {
		if rt, err = s.routing.Compile(ctx); err != nil {
			return err
		}
	}
	all := inbounds
	inbounds = onNode(all, 0)
	hostOf := s.serverHosts(ctx)
	s.applyNodes(ctx, all, active, rt, hostOf)
	rt = rt.ForServer(routing.MainServer, s.mainHost, hostOf)
	// Один 443 на Reality Xray и sing-box — через распределитель по SNI.
	inbounds, sniCfg := compile.SharePorts(inbounds)
	xrayCfg, err := compile.Xray(inbounds, active, s.apiAddr, rt)
	if err != nil {
		return err
	}
	hyCfg, err := compile.Hysteria(inbounds, active, rt)
	if err != nil {
		return err
	}
	sbCfg, err := compile.SingBox(inbounds, active, rt)
	if err != nil {
		return err
	}
	err = s.agent.Apply(ctx, agent.NodeState{Configs: map[string][]byte{
		"xray": xrayCfg, "hysteria": hyCfg, "singbox": sbCfg, "sniproxy": sniCfg,
	}})
	s.applyHops(ctx, inbounds)
	return err
}

// applyHops sends the port hopping ranges of enabled Hysteria2 inbounds to
// the network helper (which ignores an unchanged list).
func (s *Syncer) applyHops(ctx context.Context, inbounds []store.Inbound) {
	if s.hops == nil {
		return
	}
	hops := []nethelper.Hop{}
	for _, ib := range inbounds {
		h := ib.Settings.Hysteria
		if !ib.Enabled || h == nil || h.HopPorts == "" {
			continue
		}
		lo, hi, _ := strings.Cut(h.HopPorts, "-")
		a, _ := strconv.Atoi(lo)
		b, _ := strconv.Atoi(hi)
		hops = append(hops, nethelper.Hop{From: a, To: b, Target: ib.Port})
	}
	err := s.hops.SetHops(ctx, hops)
	if errors.Is(err, nethelper.ErrNoHelper) && len(hops) == 0 {
		err = nil // nothing asked for; a missing helper does not matter
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if msg != s.hopErr {
		s.hopErr = msg
		if msg != "" {
			s.log.Warn("port hopping", "err", msg)
			s.events.Add(ctx, EventHopFailed, "warning", 0, "", map[string]any{"error": msg})
		}
	}
}

const EventHopFailed = "hop.failed"

// SetNodes подключает другие наши серверы. Вызывать до Run.
func (s *Syncer) SetNodes(n *Nodes) { s.nodes = n }

func onNode(list []store.Inbound, node int64) []store.Inbound {
	out := []store.Inbound{}
	for _, ib := range list {
		if ib.NodeID == node {
			out = append(out, ib)
		}
	}
	return out
}

// applyNodes собирает конфиги для каждого узла. Узел, который ещё ни разу
// не приходил, ждёт: без адреса его API Xray конфиг не собрать.
func (s *Syncer) applyNodes(ctx context.Context, all []store.Inbound, active []store.User, rt compile.Routing, hostOf func(string) string) {
	if s.nodes == nil {
		return
	}
	nodes, err := s.store.ListNodes(ctx)
	if err != nil {
		s.log.Warn("list nodes", "err", err)
		return
	}
	nrt := rt.ForNode()
	var files []string
	if s.routing != nil {
		g := s.routing.GeoDir()
		files = append(files, filepath.Join(g, "geoip.dat"), filepath.Join(g, "geosite.dat"))
		if nrt.GeoDir != "" && nrt.GeoDir != g {
			files = append(files, filepath.Join(nrt.GeoDir, "geoip.dat"), filepath.Join(nrt.GeoDir, "geosite.dat"))
		}
	}
	for _, node := range nodes {
		api := s.nodes.XrayAPI(node.ID)
		if api == "" {
			continue
		}
		ibs, sniCfg := compile.SharePorts(onNode(all, node.ID))
		srt := nrt.ForServer(strconv.FormatInt(node.ID, 10), node.Host, hostOf)
		xr, err1 := compile.Xray(ibs, active, api, srt)
		hy, err2 := compile.Hysteria(ibs, active, srt)
		sb, err3 := compile.SingBox(ibs, active, srt)
		if err := errors.Join(err1, err2, err3); err != nil {
			s.log.Warn("compile node config", "node", node.Name, "err", err)
			continue
		}
		s.nodes.SetDesired(node.ID, map[string][]byte{"xray": xr, "hysteria": hy, "singbox": sb, "sniproxy": sniCfg}, files)
	}
}

// SetMainHost — адрес этого сервера, на который другие наши серверы
// отправляют каскадный трафик. Вызывать до Run.
func (s *Syncer) SetMainHost(h string) { s.mainHost = h }

// serverHosts — адрес сервера по его имени в выходах ("main" или id узла).
func (s *Syncer) serverHosts(ctx context.Context) func(string) string {
	hosts := map[string]string{routing.MainServer: s.mainHost}
	if nodes, err := s.store.ListNodes(ctx); err == nil {
		for _, n := range nodes {
			hosts[strconv.FormatInt(n.ID, 10)] = n.Host
		}
	}
	return func(server string) string { return hosts[server] }
}
