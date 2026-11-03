package agent

import (
	"context"
	"encoding/json"
)

// Adapter drives one VPN core on the node (Xray, Hysteria2, ...). Apply must
// be idempotent: the agent always passes the full desired state.
type Adapter interface {
	Protocol() string
	Apply(ctx context.Context, inbounds []Inbound, users []UserAccess) error
	Traffic(ctx context.Context) ([]TrafficDelta, error) // deltas since the previous call
	Online(ctx context.Context) ([]OnlineEntry, error)
	Health(ctx context.Context) HealthStatus
}

type Inbound struct {
	Tag      string          `json:"tag"`
	Protocol string          `json:"protocol"`
	Port     int             `json:"port"`
	Settings json.RawMessage `json:"settings"`
}

type UserAccess struct {
	UserID      int64           `json:"userId"`
	Email       string          `json:"email"` // stable per-user tag used for stats
	InboundTag  string          `json:"inboundTag"`
	Credentials json.RawMessage `json:"credentials"`
}

type TrafficDelta struct {
	UserID     int64  `json:"userId"`
	InboundTag string `json:"inboundTag"`
	Up         int64  `json:"up"`
	Down       int64  `json:"down"`
}

type OnlineEntry struct {
	UserID     int64  `json:"userId"`
	InboundTag string `json:"inboundTag"`
	IP         string `json:"ip"`
}

type HealthStatus struct {
	Running bool   `json:"running"`
	Version string `json:"version"`
	Message string `json:"message,omitempty"`
}
