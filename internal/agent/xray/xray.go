// Package xray supervises an xray-core process: it validates every new config
// with `xray run -test`, swaps it in, rolls back if the core fails to come
// up, restarts it after crashes and reads per-user traffic through the
// `xray api statsquery` CLI (no gRPC client code needed).
package xray

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"panel/internal/agent"
)

var errNotInstalled = errors.New("xray is not installed")

type Adapter struct {
	bin     string // path to the xray binary; geoip/geosite .dat files live next to it
	dir     string // where config files are written
	apiAddr string // 127.0.0.1:port of the stats API
	log     *slog.Logger

	mu      sync.Mutex
	proc    *process
	current []byte // config the running process uses
	lastErr string
	version string
}

type process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func New(bin, dir, apiAddr string, log *slog.Logger) *Adapter {
	return &Adapter{bin: bin, dir: dir, apiAddr: apiAddr, log: log.With("source", "xray")}
}

func (a *Adapter) Name() string { return "xray" }

func (a *Adapter) Apply(ctx context.Context, cfg []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if cfg == nil {
		a.stopLocked()
		a.current, a.lastErr = nil, ""
		return nil
	}
	if a.proc != nil && bytes.Equal(cfg, a.current) {
		return nil
	}
	if !a.installed() {
		a.lastErr = "Xray is not installed (" + a.bin + ")"
		return errNotInstalled
	}
	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		return err
	}

	live := filepath.Join(a.dir, "config.json")
	next := filepath.Join(a.dir, "config.next.json")
	prev := filepath.Join(a.dir, "config.prev.json")
	if err := os.WriteFile(next, cfg, 0o600); err != nil {
		return err
	}
	if out, err := a.run(ctx, 20*time.Second, "run", "-test", "-c", next); err != nil {
		a.lastErr = "config rejected: " + tail(out, err)
		return errors.New(a.lastErr)
	}

	hadLive := fileExists(live)
	if hadLive {
		if err := os.Rename(live, prev); err != nil {
			return err
		}
	}
	if err := os.Rename(next, live); err != nil {
		return err
	}

	a.stopLocked()
	startErr := a.startLocked(live)
	if startErr == nil {
		a.current, a.lastErr = cfg, ""
		a.log.Info("xray config applied")
		return nil
	}

	// The new config passed -test but the core did not stay up (port in use,
	// missing permissions...). Put the previous config back.
	a.current = nil
	if hadLive {
		os.Rename(prev, live)
		if err := a.startLocked(live); err == nil {
			a.current, _ = os.ReadFile(live)
		}
	}
	a.lastErr = "xray failed to start with the new config, previous config restored: " + startErr.Error()
	return errors.New(a.lastErr)
}

func (a *Adapter) startLocked(configPath string) error {
	cmd := exec.Command(a.bin, "run", "-c", configPath)
	cmd.Env = a.env()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	p := &process{cmd: cmd, done: make(chan struct{})}
	var lastLines lineRing
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			line := sc.Text()
			lastLines.add(line)
			a.log.Info(line)
		}
	}()
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()

	select {
	case <-p.done:
		return fmt.Errorf("exited immediately (%v): %s", p.err, lastLines.String())
	case <-time.After(1500 * time.Millisecond):
	}
	a.proc = p
	go a.watch(p, configPath)
	return nil
}

// watch restarts the core if it dies on its own.
func (a *Adapter) watch(p *process, configPath string) {
	<-p.done
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.proc != p {
		return // stopped on purpose
	}
	a.proc = nil
	a.lastErr = fmt.Sprintf("xray exited unexpectedly: %v", p.err)
	a.log.Error(a.lastErr)
	go func() {
		time.Sleep(5 * time.Second)
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.proc == nil && a.current != nil {
			if err := a.startLocked(configPath); err != nil {
				a.lastErr = "restart failed: " + err.Error()
				a.log.Error(a.lastErr)
			} else {
				a.lastErr = ""
				a.log.Info("xray restarted")
			}
		}
	}()
}

func (a *Adapter) stopLocked() {
	p := a.proc
	if p == nil {
		return
	}
	a.proc = nil
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		p.cmd.Process.Kill() // Windows has no SIGTERM
	}
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		p.cmd.Process.Kill()
		<-p.done
	}
}

func (a *Adapter) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopLocked()
}

func (a *Adapter) Traffic(ctx context.Context) ([]agent.TrafficDelta, error) {
	a.mu.Lock()
	running := a.proc != nil
	a.mu.Unlock()
	if !running {
		return nil, nil
	}
	out, err := a.run(ctx, 10*time.Second, "api", "statsquery", "--server="+a.apiAddr, "-pattern", "user>>>", "-reset")
	if err != nil {
		return nil, fmt.Errorf("statsquery: %s", tail(out, err))
	}
	return parseStats(out)
}

func (a *Adapter) Health(ctx context.Context) agent.HealthStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := agent.HealthStatus{Installed: a.installed(), Running: a.proc != nil, Message: a.lastErr}
	if h.Installed && a.version == "" {
		if out, err := a.run(ctx, 5*time.Second, "version"); err == nil {
			if f := strings.Fields(string(out)); len(f) >= 2 {
				a.version = f[1]
			}
		}
	}
	h.Version = a.version
	return h
}

func (a *Adapter) installed() bool {
	_, err := exec.LookPath(a.bin)
	return err == nil
}

func (a *Adapter) env() []string {
	return append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(a.bin))
}

func (a *Adapter) run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.bin, args...)
	cmd.Env = a.env()
	return cmd.CombinedOutput()
}

// parseStats reads `xray api statsquery` JSON. Values come as numbers or,
// from protobuf JSON, as strings; json.Number accepts both.
func parseStats(out []byte) ([]agent.TrafficDelta, error) {
	var resp struct {
		Stat []struct {
			Name  string      `json:"name"`
			Value json.Number `json:"value"`
		} `json:"stat"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse statsquery output: %w", err)
	}
	byEmail := map[string]*agent.TrafficDelta{}
	var order []string
	for _, s := range resp.Stat {
		// user>>>{email}>>>traffic>>>uplink|downlink
		parts := strings.Split(s.Name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
			continue
		}
		v, _ := s.Value.Int64()
		if v == 0 {
			continue
		}
		d := byEmail[parts[1]]
		if d == nil {
			d = &agent.TrafficDelta{Email: parts[1]}
			byEmail[parts[1]] = d
			order = append(order, parts[1])
		}
		switch parts[3] {
		case "uplink":
			d.Up += v
		case "downlink":
			d.Down += v
		}
	}
	deltas := make([]agent.TrafficDelta, 0, len(order))
	for _, e := range order {
		deltas = append(deltas, *byEmail[e])
	}
	return deltas, nil
}

func tail(out []byte, err error) string {
	s := strings.TrimSpace(string(out))
	if len(s) > 600 {
		s = "…" + s[len(s)-600:]
	}
	if s == "" && err != nil {
		s = err.Error()
	}
	return s
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// lineRing keeps the last few output lines to explain a failed start.
type lineRing struct {
	mu    sync.Mutex
	lines []string
}

func (r *lineRing) add(l string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, l)
	if len(r.lines) > 5 {
		r.lines = r.lines[1:]
	}
}

func (r *lineRing) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.lines, " | ")
}
