package nethelper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

type SpeedLimit struct {
	Mark uint32 `json:"mark"`
	Mbps int    `json:"mbps"`
}

func validSpeedLimits(limits []SpeedLimit) error {
	if len(limits) > 10000 {
		return errors.New("too many speed limits")
	}
	seen := map[uint32]bool{}
	for _, l := range limits {
		if l.Mbps < 1 || l.Mbps > 1000000 || l.Mark&0xff000000 != 0x50000000 {
			return fmt.Errorf("invalid speed limit")
		}
		if seen[l.Mark] {
			return fmt.Errorf("duplicate packet mark")
		}
		seen[l.Mark] = true
	}
	return nil
}

func speedLimitBatch(iface string) string {
	return fmt.Sprintf("qdisc del dev %s ingress\nqdisc del dev %s root\nqdisc add dev %s root fq\n", iface, iface, iface)
}

func speedLimitRuleset(limits []SpeedLimit) string {
	var b strings.Builder
	b.WriteString("table inet panel_speed\ndelete table inet panel_speed\ntable netdev panel_speed\ndelete table netdev panel_speed\n")
	if len(limits) == 0 {
		return b.String()
	}
	b.WriteString("table inet panel_speed {\n\tchain output {\n\t\ttype filter hook output priority mangle; policy accept;\n\t\tmeta mark & 0xff000000 == 0x50000000 ct mark set meta mark\n\t}\n")
	b.WriteString("\tchain input {\n\t\ttype filter hook prerouting priority filter; policy accept;\n")
	for _, l := range limits {
		bytesPerSecond := int64(l.Mbps) * 1000000 / 8
		fmt.Fprintf(&b, "\t\tct mark 0x%x limit rate over %d bytes/second burst 32 kbytes counter drop\n", l.Mark, bytesPerSecond)
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

func defaultInterface(ctx context.Context) (string, error) {
	for _, args := range [][]string{{"route", "show", "default"}, {"-6", "route", "show", "default"}} {
		out, err := exec.CommandContext(ctx, "ip", args...).Output()
		if err != nil {
			continue
		}
		fields := strings.Fields(string(out))
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "dev" && validIface(fields[i+1]) {
				return fields[i+1], nil
			}
		}
	}
	return "", errors.New("cannot find the default network interface")
}

func validIface(s string) bool {
	if s == "" || len(s) > 15 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func removeLimitInterfaces(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "ip", "-o", "link", "show").Output()
	if err != nil {
		return fmt.Errorf("list IFB interfaces: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name, _, _ := strings.Cut(fields[1], "@")
		if !strings.HasPrefix(name, "ifb-p") && name != "ifb-panel" {
			continue
		}
		if out, err := exec.CommandContext(ctx, "ip", "link", "del", "dev", name).CombinedOutput(); err != nil {
			return fmt.Errorf("remove IFB %s: %s", name, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func missingQdiscOutput(out string) bool {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Command failed -:") ||
			line == "Error: Invalid handle." || line == "Error: No such file or directory." {
			continue
		}
		return false
	}
	return true
}

func applySpeedLimits(ctx context.Context, limits []SpeedLimit) error {
	if _, err := exec.LookPath("tc"); err != nil {
		return errors.New("tc is not installed (apt install iproute2)")
	}
	if _, err := exec.LookPath("nft"); err != nil {
		return errors.New("nft is not installed (apt install nftables)")
	}
	iface, err := defaultInterface(ctx)
	if err != nil {
		return err
	}
	if err := removeLimitInterfaces(ctx); err != nil {
		return err
	}
	nft := exec.CommandContext(ctx, "nft", "-f", "-")
	nft.Stdin = strings.NewReader(speedLimitRuleset(limits))
	if out, err := nft.CombinedOutput(); err != nil {
		return fmt.Errorf("nft speed marks: %s", strings.TrimSpace(string(out)))
	}
	cmd := exec.CommandContext(ctx, "tc", "-force", "-batch", "-")
	cmd.Stdin = strings.NewReader(speedLimitBatch(iface))
	if out, err := cmd.CombinedOutput(); err != nil && !missingQdiscOutput(string(out)) {
		return fmt.Errorf("tc: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

type limitState struct {
	mu      sync.Mutex
	current []SpeedLimit
}

func (s *limitState) handle(log *slog.Logger, w http.ResponseWriter, r *http.Request) {
	var limits []SpeedLimit
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&limits); err != nil || validSpeedLimits(limits) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	slices.SortFunc(limits, func(a, b SpeedLimit) int { return int(a.Mark) - int(b.Mark) })
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Equal(limits, s.current) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	c, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := applySpeedLimits(c, limits); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.current = limits
	log.Info("speed limits applied", "users", len(limits))
	w.WriteHeader(http.StatusNoContent)
}

func (c *Client) SetSpeedLimits(ctx context.Context, limits []SpeedLimit) error {
	if limits == nil {
		limits = []SpeedLimit{}
	}
	body, _ := json.Marshal(limits)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, "http://helper/limits", bytes.NewReader(body))
	resp, err := c.http.Do(req)
	if err != nil {
		var ne *net.OpError
		if errors.As(err, &ne) {
			return ErrNoHelper
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return errors.New(strings.TrimSpace(string(b)))
	}
	return nil
}
