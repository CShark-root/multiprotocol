// Package agent runs on every VPN server and drives its cores. In
// single-server mode the panel embeds it in-process; the standalone binary
// with gRPC + mTLS arrives with multi-node support (PLAN.md phase 3).
package agent

import (
	"context"
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

type NodeHealth struct {
	Hostname           string       `json:"hostname"`
	OS                 string       `json:"os"`
	Arch               string       `json:"arch"`
	AgentUptimeSeconds int64        `json:"agentUptimeSeconds"`
	System             SystemInfo   `json:"system"`
	Cores              []CoreHealth `json:"cores"`
}

type CoreHealth struct {
	Protocol string `json:"protocol"`
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
		h.Cores = append(h.Cores, CoreHealth{Protocol: ad.Protocol(), HealthStatus: ad.Health(ctx)})
	}
	return h
}
