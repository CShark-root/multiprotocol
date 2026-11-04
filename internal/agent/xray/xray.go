// Package xray drives an xray-core process through core.Runner: configs are
// checked with `xray run -test` before they go live, and per-user traffic is
// read through the `xray api statsquery` CLI (no gRPC client code needed).
package xray

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"panel/internal/agent"
	"panel/internal/agent/core"
)

type Adapter struct {
	runner  *core.Runner
	apiAddr string // 127.0.0.1:port of the stats API

	mu      sync.Mutex
	version string
}

// New expects the geoip/geosite .dat files next to bin.
func New(bin, dir, apiAddr string, log *slog.Logger) *Adapter {
	return &Adapter{
		apiAddr: apiAddr,
		runner: core.NewRunner(core.Spec{
			Name:     "xray",
			Bin:      bin,
			Dir:      dir,
			Config:   "config.json",
			Args:     []string{"run", "-c", "{config}"},
			TestArgs: []string{"run", "-test", "-c", "{config}"},
			Env:      []string{"XRAY_LOCATION_ASSET=" + filepath.Dir(bin)},
			Log:      log.With("source", "xray"),
		}),
	}
}

func (a *Adapter) Name() string { return "xray" }

func (a *Adapter) Apply(ctx context.Context, cfg []byte) error { return a.runner.Apply(ctx, cfg) }

func (a *Adapter) Stop() { a.runner.Stop() }

func (a *Adapter) Traffic(ctx context.Context) ([]agent.TrafficDelta, error) {
	if !a.runner.Running() {
		return nil, nil
	}
	out, err := a.runner.Run(ctx, 10*time.Second, "api", "statsquery", "--server="+a.apiAddr, "-pattern", "user>>>", "-reset")
	if err != nil {
		return nil, fmt.Errorf("statsquery: %s", core.Tail(out, err))
	}
	return parseStats(out)
}

// Online lists users with live sessions and their IPs (policy
// statsUserOnline must be on, see compile.Xray).
func (a *Adapter) Online(ctx context.Context) ([]agent.OnlineEntry, error) {
	if !a.runner.Running() {
		return nil, nil
	}
	out, err := a.runner.Run(ctx, 10*time.Second, "api", "statsonlineiplist", "--server="+a.apiAddr, "-all")
	if err != nil {
		return nil, fmt.Errorf("statsonlineiplist: %s", core.Tail(out, err))
	}
	return parseOnline(out)
}

func (a *Adapter) Health(ctx context.Context) agent.HealthStatus {
	h := agent.HealthStatus{Installed: a.runner.Installed(), Running: a.runner.Running(), Message: a.runner.LastError()}
	a.mu.Lock()
	defer a.mu.Unlock()
	if h.Installed && a.version == "" {
		if out, err := a.runner.Run(ctx, 5*time.Second, "version"); err == nil {
			if f := strings.Fields(string(out)); len(f) >= 2 {
				a.version = f[1]
			}
		}
	}
	h.Version = a.version
	return h
}

// parseOnline reads `xray api statsonlineiplist -all`:
// {"users":[{"email":"u3","ips":[{"ip":"1.2.3.4","lastSeen":1793791460}]}]}.
// An empty result is "{}". Xray keeps an IP while at least one of its
// connections is open, so the IP count stands in for the connection count.
func parseOnline(out []byte) ([]agent.OnlineEntry, error) {
	var resp struct {
		Users []struct {
			Email string `json:"email"`
			IPs   []struct {
				IP string `json:"ip"`
			} `json:"ips"`
		} `json:"users"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse statsonlineiplist output: %w", err)
	}
	entries := make([]agent.OnlineEntry, 0, len(resp.Users))
	for _, u := range resp.Users {
		if u.Email == "" || len(u.IPs) == 0 {
			continue
		}
		e := agent.OnlineEntry{Email: u.Email, Connections: len(u.IPs)}
		for _, ip := range u.IPs {
			e.IPs = append(e.IPs, ip.IP)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// parseStats reads `xray api statsquery` JSON. Values come as numbers or,
// from protobuf JSON, as strings; json.Number accepts both.
func parseStats(out []byte) ([]agent.TrafficDelta, error) {
	var resp struct {
		Stat []struct {
			Name  string      `json:"name"`
			Value json.Number `json:"value"`
		} `json:"stat"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse statsquery output: %w", err)
	}
	byEmail := map[string]*agent.TrafficDelta{}
	var order []string
	for _, s := range resp.Stat {
		// user>>>{email}>>>traffic>>>uplink|downlink
		parts := strings.Split(s.Name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
			continue
		}
		v, _ := s.Value.Int64()
		if v == 0 {
			continue
		}
		d := byEmail[parts[1]]
		if d == nil {
			d = &agent.TrafficDelta{Email: parts[1]}
			byEmail[parts[1]] = d
			order = append(order, parts[1])
		}
		switch parts[3] {
		case "uplink":
			d.Up += v
		case "downlink":
			d.Down += v
		}
	}
	deltas := make([]agent.TrafficDelta, 0, len(order))
	for _, e := range order {
		deltas = append(deltas, *byEmail[e])
	}
	return deltas, nil
}
