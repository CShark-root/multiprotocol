//go:build linux

package agent

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"syscall"
)

// readExtra fills the Server tab details from /proc and /etc.
func readExtra(info *SystemInfo) {
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(b))
		for sc.Scan() {
			if v, ok := strings.CutPrefix(sc.Text(), "PRETTY_NAME="); ok {
				info.OSName = strings.Trim(v, `"`)
			}
		}
	}
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		info.Kernel = strings.TrimSpace(string(b))
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
			case "SwapTotal:":
				info.SwapTotal = kb * 1024
			case "SwapFree:":
				info.SwapFree = kb * 1024
			}
		}
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(diskPath(), &st); err == nil {
		info.DiskTotal = st.Blocks * uint64(st.Bsize)
		info.DiskFree = st.Bavail * uint64(st.Bsize)
	}
	if b, err := os.ReadFile("/proc/sys/net/ipv4/tcp_congestion_control"); err == nil {
		info.Congestion = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile("/proc/sys/net/core/default_qdisc"); err == nil {
		info.Qdisc = strings.TrimSpace(string(b))
	}
	if entries, err := os.ReadDir("/proc"); err == nil {
		for _, e := range entries {
			if n := e.Name(); n != "" && n[0] >= '0' && n[0] <= '9' {
				info.Processes++
			}
		}
	}
}

// diskPath is where the panel keeps its data (the disk that can fill up).
func diskPath() string {
	if _, err := os.Stat("/var/lib/panel"); err == nil {
		return "/var/lib/panel"
	}
	return "/"
}
