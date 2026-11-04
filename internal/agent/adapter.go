package agent

import "context"

// Adapter drives one VPN core on the node (Xray, Hysteria2, sing-box). The
// panel compiles the final config; the adapter validates, applies and
// supervises it.
type Adapter interface {
	Name() string
	// Apply makes the core run config. It is idempotent; a nil config stops
	// the core. On failure the previous working config stays active.
	Apply(ctx context.Context, config []byte) error
	// Traffic returns per-user byte counts since the previous call.
	Traffic(ctx context.Context) ([]TrafficDelta, error)
	// Online lists users connected right now.
	Online(ctx context.Context) ([]OnlineEntry, error)
	Health(ctx context.Context) HealthStatus
	Stop()
}

type TrafficDelta struct {
	Email string `json:"email"`
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
}

// OnlineEntry is one connected user as a core sees them. IPs may be empty
// when the core does not report them (Hysteria reports only a count).
type OnlineEntry struct {
	Email       string   `json:"email"`
	IPs         []string `json:"ips"`
	Connections int      `json:"connections"`
}

type HealthStatus struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Version   string `json:"version"`
	Message   string `json:"message,omitempty"` // last error, human readable
}

// NodeState is the full desired state of a node: one config per core,
// keyed by Adapter.Name. A missing entry stops that core.
type NodeState struct {
	Configs map[string][]byte
}
