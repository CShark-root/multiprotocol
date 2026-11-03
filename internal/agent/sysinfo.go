package agent

type SystemInfo struct {
	CPUs          int     `json:"cpus"`
	UptimeSeconds int64   `json:"uptimeSeconds"`
	MemTotal      uint64  `json:"memTotal"`     // bytes
	MemAvailable  uint64  `json:"memAvailable"` // bytes
	Load1         float64 `json:"load1"`
	Load5         float64 `json:"load5"`
	Load15        float64 `json:"load15"`
}
