//go:build linux

package agent

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"runtime"
)

func readSystemInfo() SystemInfo {
	info := SystemInfo{CPUs: runtime.NumCPU()}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		var up float64
		fmt.Sscanf(string(b), "%f", &up)
		info.UptimeSeconds = int64(up)
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		fmt.Sscanf(string(b), "%f %f %f", &info.Load1, &info.Load5, &info.Load15)
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(b))
		for sc.Scan() {
			var key string
			var kb uint64
			if _, err := fmt.Sscanf(sc.Text(), "%s %d", &key, &kb); err != nil {
				continue
			}
			switch key {
			case "MemTotal:":
				info.MemTotal = kb * 1024
			case "MemAvailable:":
				info.MemAvailable = kb * 1024
			}
		}
	}
	info.NetIface, info.NetRxBps, info.NetTxBps = netRate()
	return info
}
