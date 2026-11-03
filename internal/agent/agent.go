// Package agent runs on every VPN server and drives its cores. In
// single-server mode the panel embeds it in-process; the standalone binary
// with gRPC + mTLS arrives with multi-node support (PLAN.md phase 3).
package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"
)

type Agent struct {
	adapters []Adapter
	started  time.Time
}

func New(adapters ...Adapter) *Agent {
	return &Agent{adapters: adapters, started: time.Now()}
}

// Apply pushes the desired state to every core. Cores are independent: one
// failing does not stop the others from being updated.
func (a *Agent) Apply(ctx context.Context, st NodeState) error {
	var errs []error
	for _, ad := range a.adapters {
		if err := ad.Apply(ctx, st.Configs[ad.Name()]); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ad.Name(), err))
		}
	}
	return errors.Join(errs...)
}

// Traffic collects per-user deltas from all cores.
func (a *Agent) Traffic(ctx context.Context) ([]TrafficDelta, error) {
	var all []TrafficDelta
	var errs []error
	for _, ad := range a.adapters {
		d, err := ad.Traffic(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ad.Name(), err))
			continue
		}
		all = append(all, d...)
	}
	return all, errors.Join(errs...)
}

func (a *Agent) Stop() {
	for _, ad := range a.adapters {
		ad.Stop()
	}
}

type NodeHealth struct {
	Hostname           string       `json:"hostname"`
	OS                 string       `json:"os"`
	Arch               string       `json:"arch"`
	AgentUptimeSeconds int64        `json:"agentUptimeSeconds"`
	System             SystemInfo   `json:"system"`
	Cores              []CoreHealth `json:"cores"`
}

type CoreHealth struct {
	Name string `json:"name"`
	HealthStatus
}

func (a *Agent) Health(ctx context.Context) NodeHealth {
	host, _ := os.Hostname()
	h := NodeHealth{
		Hostname:           host,
		OS:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		AgentUptimeSeconds: int64(time.Since(a.started).Seconds()),
		System:             readSystemInfo(),
		Cores:              []CoreHealth{},
	}
	for _, ad := range a.adapters {
		h.Cores = append(h.Cores, CoreHealth{Name: ad.Name(), HealthStatus: ad.Health(ctx)})
	}
	return h
}
