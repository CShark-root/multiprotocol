// Package hysteria drives official Hysteria 2 server processes, one per
// inbound. Users are not written into the config: every Hysteria process
// asks the agent's local HTTP auth endpoint, so adding or removing a user
// needs no restart. Removed users are kicked through the trafficStats API,
// which also reports per-user traffic.
package hysteria

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"panel/internal/agent"
	"panel/internal/agent/core"
)

// State is what the panel compiles for this adapter.
type State struct {
	Instances []Instance        `json:"instances"`
	Users     map[string]string `json:"users"` // password -> user email tag
}

type Instance struct {
	Tag string `json:"tag"`
	// Server is the Hysteria server config without tls file paths, auth and
	// trafficStats; those are node-local details the adapter fills in.
	Server  map[string]any `json:"server"`
	CertPEM string         `json:"certPem"`
	KeyPEM  string         `json:"keyPem"`
}

type Adapter struct {
	bin string
	dir string
	log *slog.Logger

	mu        sync.Mutex
	instances map[string]*instance
	version   string
	authURL   string // set once the auth server listens

	usersMu sync.RWMutex
	users   map[string]string
}

type instance struct {
	runner    *core.Runner
	statsAddr string
	secret    string
}

func New(bin, dir string, log *slog.Logger) *Adapter {
	return &Adapter{bin: bin, dir: dir, log: log.With("source", "hysteria"), instances: map[string]*instance{}}
}

func (a *Adapter) Name() string { return "hysteria" }

func (a *Adapter) Apply(ctx context.Context, cfg []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if cfg == nil {
		for tag, in := range a.instances {
			in.runner.Stop()
			delete(a.instances, tag)
		}
		a.setUsers(nil)
		return nil
	}
	var st State
	if err := json.Unmarshal(cfg, &st); err != nil {
		return fmt.Errorf("bad hysteria state: %w", err)
	}
	if err := a.startAuthLocked(); err != nil {
		return err
	}
	removed := a.setUsers(st.Users)

	var errs []error
	wanted := map[string]bool{}
	for _, want := range st.Instances {
		wanted[want.Tag] = true
		if err := a.applyInstance(ctx, want); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", want.Tag, err))
		}
	}
	for tag, in := range a.instances {
		if !wanted[tag] {
			in.runner.Stop()
			delete(a.instances, tag)
		}
	}
	if len(removed) > 0 {
		for _, in := range a.instances {
			if in.runner.Running() {
				if err := in.kick(ctx, removed); err != nil {
					a.log.Warn("kick removed users", "err", err)
				}
			}
		}
	}
	return errors.Join(errs...)
}

var tagRe = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

func (a *Adapter) applyInstance(ctx context.Context, want Instance) error {
	if !tagRe.MatchString(want.Tag) {
		return fmt.Errorf("invalid tag %q", want.Tag)
	}
	in := a.instances[want.Tag]
	if in == nil {
		addr, err := freeLocalAddr()
		if err != nil {
			return err
		}
		dir := filepath.Join(a.dir, want.Tag)
		in = &instance{
			statsAddr: addr,
			secret:    randomHex(16),
			runner: core.NewRunner(core.Spec{
				Name:   "hysteria " + want.Tag,
				Bin:    a.bin,
				Dir:    dir,
				Config: "config.json",
				Args:   []string{"server", "-c", "{config}"},
				Env:    []string{"HYSTERIA_LOG_LEVEL=warn", "HYSTERIA_DISABLE_UPDATE_CHECK=1"},
				Log:    a.log.With("inbound", want.Tag),
			}),
		}
		a.instances[want.Tag] = in
	}
	if !in.runner.Installed() {
		return in.runner.Apply(ctx, []byte("{}")) // records "not installed"
	}

	// File names carry a content hash, so a new certificate changes the
	// config bytes and therefore restarts the process.
	dir := filepath.Join(a.dir, want.Tag)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	certPath, err := writeHashed(dir, "cert", want.CertPEM, 0o600)
	if err != nil {
		return err
	}
	keyPath, err := writeHashed(dir, "key", want.KeyPEM, 0o600)
	if err != nil {
		return err
	}

	server := map[string]any{}
	for k, v := range want.Server {
		server[k] = v
	}
	server["tls"] = map[string]any{"cert": certPath, "key": keyPath}
	server["auth"] = map[string]any{"type": "http", "http": map[string]any{"url": a.authURL}}
	server["trafficStats"] = map[string]any{"listen": in.statsAddr, "secret": in.secret}
	cfg, err := json.MarshalIndent(server, "", "  ")
	if err != nil {
		return err
	}
	return in.runner.Apply(ctx, cfg)
}

func (a *Adapter) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, in := range a.instances {
		in.runner.Stop()
	}
}

func (a *Adapter) Traffic(ctx context.Context) ([]agent.TrafficDelta, error) {
	a.mu.Lock()
	var running []*instance
	for _, in := range a.instances {
		if in.runner.Running() {
			running = append(running, in)
		}
	}
	a.mu.Unlock()

	byEmail := map[string]*agent.TrafficDelta{}
	var errs []error
	for _, in := range running {
		stats, err := in.traffic(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for email, s := range stats {
			d := byEmail[email]
			if d == nil {
				d = &agent.TrafficDelta{Email: email}
				byEmail[email] = d
			}
			d.Up += s.Rx   // received from the client
			d.Down += s.Tx // sent to the client
		}
	}
	emails := make([]string, 0, len(byEmail))
	for e := range byEmail {
		emails = append(emails, e)
	}
	sort.Strings(emails)
	out := make([]agent.TrafficDelta, 0, len(emails))
	for _, e := range emails {
		if d := byEmail[e]; d.Up+d.Down > 0 {
			out = append(out, *d)
		}
	}
	return out, errors.Join(errs...)
}

func (a *Adapter) Health(ctx context.Context) agent.HealthStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	probe := core.NewRunner(core.Spec{Bin: a.bin})
	h := agent.HealthStatus{Installed: probe.Installed(), Running: len(a.instances) > 0}
	var msgs []string
	for tag, in := range a.instances {
		if !in.runner.Running() {
			h.Running = false
		}
		if e := in.runner.LastError(); e != "" {
			msgs = append(msgs, tag+": "+e)
		}
	}
	sort.Strings(msgs)
	h.Message = strings.Join(msgs, "; ")
	if h.Installed && a.version == "" {
		if out, err := probe.Run(ctx, 5*time.Second, "version"); err == nil {
			a.version = parseVersion(string(out))
		}
	}
	h.Version = a.version
	return h
}

// parseVersion finds "Version: v2.6.5" in `hysteria version` output.
func parseVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "Version" {
			return strings.TrimPrefix(strings.TrimSpace(v), "v")
		}
	}
	return ""
}

// setUsers swaps the auth table and returns the emails that lost access.
func (a *Adapter) setUsers(users map[string]string) []string {
	a.usersMu.Lock()
	defer a.usersMu.Unlock()
	still := map[string]bool{}
	for _, e := range users {
		still[e] = true
	}
	var removed []string
	seen := map[string]bool{}
	for _, e := range a.users {
		if !still[e] && !seen[e] {
			removed = append(removed, e)
			seen[e] = true
		}
	}
	a.users = users
	return removed
}

// startAuthLocked starts the auth endpoint Hysteria calls for every new
// connection. It listens on loopback under a random path, so other local
// processes cannot use it to test passwords.
func (a *Adapter) startAuthLocked() error {
	if a.authURL != "" {
		return nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("auth endpoint: %w", err)
	}
	path := "/auth/" + randomHex(16)
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+path, a.handleAuth)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	a.authURL = "http://" + ln.Addr().String() + path
	return nil
}

func (a *Adapter) handleAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Addr string `json:"addr"`
		Auth string `json:"auth"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	a.usersMu.RLock()
	email, ok := a.users[req.Auth]
	a.usersMu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": ok, "id": email})
}

type userStats struct {
	Tx int64 `json:"tx"`
	Rx int64 `json:"rx"`
}

var statsClient = &http.Client{Timeout: 10 * time.Second}

func (in *instance) traffic(ctx context.Context) (map[string]userStats, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+in.statsAddr+"/traffic?clear=1", nil)
	req.Header.Set("Authorization", in.secret)
	resp, err := statsClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trafficStats: HTTP %d", resp.StatusCode)
	}
	var stats map[string]userStats
	return stats, json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&stats)
}

func (in *instance) kick(ctx context.Context, emails []string) error {
	body, _ := json.Marshal(emails)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+in.statsAddr+"/kick", bytes.NewReader(body))
	req.Header.Set("Authorization", in.secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := statsClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kick: HTTP %d", resp.StatusCode)
	}
	return nil
}

func writeHashed(dir, name, content string, mode os.FileMode) (string, error) {
	sum := sha256.Sum256([]byte(content))
	path := filepath.Join(dir, fmt.Sprintf("%s-%x.pem", name, sum[:6]))
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return "", err
	}
	return path, nil
}

func freeLocalAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return l.Addr().String(), nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
