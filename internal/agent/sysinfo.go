package agent

type SystemInfo struct {
	CPUs          int     `json:"cpus"`
	UptimeSeconds int64   `json:"uptimeSeconds"`
	MemTotal      uint64  `json:"memTotal"`     // bytes
	MemAvailable  uint64  `json:"memAvailable"` // bytes
	Load1         float64 `json:"load1"`
	Load5         float64 `json:"load5"`
	Load15        float64 `json:"load15"`
	// Throughput of the interface with the default route, bytes per second,
	// averaged since the previous reading (the dashboard polls every few
	// seconds). NetIface is empty when unknown.
	NetIface string  `json:"netIface"`
	NetRxBps float64 `json:"netRxBps"`
	NetTxBps float64 `json:"netTxBps"`
}
