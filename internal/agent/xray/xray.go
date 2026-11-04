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
