//go:build !linux

package agent

import "runtime"

// Non-Linux builds exist only for development; real nodes run Linux.
func readSystemInfo() SystemInfo {
	return SystemInfo{CPUs: runtime.NumCPU()}
}
