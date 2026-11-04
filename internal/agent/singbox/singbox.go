// Package singbox drives a sing-box server process (TUIC, AnyTLS,
// ShadowTLS) through core.Runner. Configs are checked with `sing-box check`.
//
// Per-user traffic: the release build has no v2ray stats API, and the Clash
// API does not say which user opened a connection. The panel therefore
// routes each user through their own outbound ("direct-u7"), and this
// adapter polls the Clash API's connection list, attributing byte counts by
// that outbound. Bytes a connection transfers after the last poll before it
// closes (up to one poll interval's worth) are not counted, so a burst
// shorter than a second can be missed entirely; long-lived connections
// (browsing, streaming) are counted almost exactly. Exact accounting needs a
// sing-box build with the v2ray stats API (PLAN.md, phase 2).
package singbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"panel/internal/agent"
	"panel/internal/agent/core"
)

// The Clash API is on loopback and the list is small, so polling every
// second is cheap and keeps the uncounted tail short.
const pollInterval = time.Second

// userOutboundPrefix must match compile.SingBoxUserOutbound.
const userOutboundPrefix = "direct-"

type Adapter struct {
	runner *core.Runner
	log    *slog.Logger

	mu        sync.Mutex
	clashAddr string
	secret    string
	polling   bool
	version   string

	statsMu sync.Mutex
	conns   map[string]connState // by connection id
	pending map[string]*agent.TrafficDelta
}

type connState struct{ up, down int64 }

func New(bin, dir string, log *slog.Logger) *Adapter {
	log = log.With("source", "sing-box")
	return &Adapter{
		log: log,
		runner: core.NewRunner(core.Spec{
			Name:     "sing-box",
			Bin:      bin,
			Dir:      dir,
			Config:   "config.json",
			Args:     []string{"run", "-c", "{config}", "--disable-color"},
			TestArgs: []string{"check", "-c", "{config}"},
			Log:      log,
		}),
		conns:   map[string]connState{},
		pending: map[string]*agent.TrafficDelta{},
	}
}

func (a *Adapter) Name() string { return "singbox" }

func (a *Adapter) Apply(ctx context.Context, cfg []byte) error {
	if cfg == nil {
		return a.runner.Apply(ctx, nil)
	}
	var m map[string]any
	if err := json.Unmarshal(cfg, &m); err != nil {
		return fmt.Errorf("bad sing-box config: %w", err)
	}
	a.mu.Lock()
	if a.clashAddr == "" {
		addr, err := freeLocalAddr()
		if err != nil {
			a.mu.Unlock()
			return err
		}
		a.clashAddr, a.secret = addr, randomHex(16)
	}
	m["experimental"] = map[string]any{
		"clash_api": map[string]any{"external_controller": a.clashAddr, "secret": a.secret},
	}
	startPoll := !a.polling
	a.polling = true
	a.mu.Unlock()

	final, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if startPoll {
		go a.poll()
	}
	return a.runner.Apply(ctx, final)
}

func (a *Adapter) Stop() { a.runner.Stop() }

func (a *Adapter) Traffic(context.Context) ([]agent.TrafficDelta, error) {
	a.statsMu.Lock()
	defer a.statsMu.Unlock()
	out := make([]agent.TrafficDelta, 0, len(a.pending))
	for _, d := range a.pending {
		if d.Up+d.Down > 0 {
			out = append(out, *d)
		}
	}
	a.pending = map[string]*agent.TrafficDelta{}
	return out, nil
}

func (a *Adapter) Health(ctx context.Context) agent.HealthStatus {
	h := agent.HealthStatus{Installed: a.runner.Installed(), Running: a.runner.Running(), Message: a.runner.LastError()}
	a.mu.Lock()
	defer a.mu.Unlock()
	if h.Installed && a.version == "" {
		if out, err := a.runner.Run(ctx, 5*time.Second, "version"); err == nil {
			// "sing-box version 1.14.4"
			if f := strings.Fields(strings.SplitN(string(out), "\n", 2)[0]); len(f) >= 3 {
				a.version = f[2]
			}
		}
	}
	h.Version = a.version
	return h
}

var client = &http.Client{Timeout: 5 * time.Second}

func (a *Adapter) poll() {
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for range t.C {
		if !a.runner.Running() {
			a.statsMu.Lock()
			a.conns = map[string]connState{}
			a.statsMu.Unlock()
			continue
		}
		if err := a.pollOnce(); err != nil {
			a.log.Debug("clash api poll", "err", err)
		}
	}
}

type connections struct {
	Connections []connEntry `json:"connections"`
}

type connEntry struct {
	ID       string   `json:"id"`
	Upload   int64    `json:"upload"`
	Download int64    `json:"download"`
	Chains   []string `json:"chains"`
}

func (a *Adapter) pollOnce() error {
	a.mu.Lock()
	addr, secret := a.clashAddr, a.secret
	a.mu.Unlock()

	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/connections", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var c connections
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&c); err != nil {
		return err
	}
	a.account(c)
	return nil
}

// account turns cumulative per-connection counters into per-user deltas.
func (a *Adapter) account(c connections) {
	a.statsMu.Lock()
	defer a.statsMu.Unlock()
	seen := make(map[string]connState, len(c.Connections))
	for _, conn := range c.Connections {
		cur := connState{conn.Upload, conn.Download}
		seen[conn.ID] = cur
		if len(conn.Chains) == 0 || !strings.HasPrefix(conn.Chains[0], userOutboundPrefix) {
			continue
		}
		email := strings.TrimPrefix(conn.Chains[0], userOutboundPrefix)
		prev := a.conns[conn.ID]
		d := a.pending[email]
		if d == nil {
			d = &agent.TrafficDelta{Email: email}
			a.pending[email] = d
		}
		d.Up += max(0, cur.up-prev.up)
		d.Down += max(0, cur.down-prev.down)
	}
	a.conns = seen
}

func freeLocalAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return l.Addr().String(), nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
