// Package core supervises one VPN core process: it validates a new config
// (when the core has a test mode), swaps it in, rolls back if the core does
// not stay up, and restarts it after crashes. Xray and Hysteria adapters
// build on it.
package core

import (
	"bufio"
	"bytes"
	"context"
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
)

var ErrNotInstalled = errors.New("core binary is not installed")

type Spec struct {
	Name     string   // for messages: "xray", "hysteria in-3"
	Bin      string   // absolute path or name on PATH
	Dir      string   // where config files are written
	Config   string   // config file name inside Dir, e.g. "config.json"
	Args     []string // run arguments; "{config}" is replaced with the config path
	TestArgs []string // optional validation command, same placeholder
	Env      []string // extra environment
	Log      *slog.Logger
}

type Runner struct {
	spec Spec

	mu      sync.Mutex
	proc    *process
	current []byte // config the running process uses
	lastErr string
}

type process struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func NewRunner(spec Spec) *Runner { return &Runner{spec: spec} }

func (r *Runner) Installed() bool {
	_, err := exec.LookPath(r.spec.Bin)
	return err == nil
}

func (r *Runner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.proc != nil
}

func (r *Runner) LastError() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}

// Apply makes the core run cfg; nil stops it. On failure the previous
// working config stays active and LastError explains what happened.
func (r *Runner) Apply(ctx context.Context, cfg []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cfg == nil {
		r.stopLocked()
		r.current, r.lastErr = nil, ""
		return nil
	}
	if r.proc != nil && bytes.Equal(cfg, r.current) {
		return nil
	}
	if !r.Installed() {
		r.lastErr = fmt.Sprintf("%s is not installed (%s)", r.spec.Name, r.spec.Bin)
		return ErrNotInstalled
	}
	if err := os.MkdirAll(r.spec.Dir, 0o700); err != nil {
		return err
	}

	live := filepath.Join(r.spec.Dir, r.spec.Config)
	ext := filepath.Ext(r.spec.Config)
	base := strings.TrimSuffix(live, ext)
	next, prev := base+".next"+ext, base+".prev"+ext
	if err := os.WriteFile(next, cfg, 0o600); err != nil {
		return err
	}
	if r.spec.TestArgs != nil {
		if out, err := r.Run(ctx, 20*time.Second, expand(r.spec.TestArgs, next)...); err != nil {
			r.lastErr = "config rejected: " + Tail(out, err)
			return errors.New(r.lastErr)
		}
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

	r.stopLocked()
	startErr := r.startLocked(live)
	if startErr == nil {
		r.current, r.lastErr = cfg, ""
		r.spec.Log.Info("config applied")
		return nil
	}

	// The new config did not keep the core up (port in use, bad cert...).
	// Put the previous one back.
	r.current = nil
	if hadLive {
		os.Rename(prev, live)
		if err := r.startLocked(live); err == nil {
			r.current, _ = os.ReadFile(live)
		}
	}
	r.lastErr = r.spec.Name + " failed to start with the new config, previous config restored: " + startErr.Error()
	return errors.New(r.lastErr)
}

func (r *Runner) startLocked(configPath string) error {
	cmd := exec.Command(r.spec.Bin, expand(r.spec.Args, configPath)...)
	cmd.Env = append(os.Environ(), r.spec.Env...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	p := &process{cmd: cmd, done: make(chan struct{})}
	var last lineRing
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			line := sc.Text()
			last.add(line)
			r.spec.Log.Info(line)
		}
	}()
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()

	select {
	case <-p.done:
		return fmt.Errorf("exited immediately (%v): %s", p.err, last.String())
	case <-time.After(1500 * time.Millisecond):
	}
	r.proc = p
	go r.watch(p, configPath)
	return nil
}

// watch restarts the core if it dies on its own.
func (r *Runner) watch(p *process, configPath string) {
	<-p.done
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.proc != p {
		return // stopped on purpose
	}
	r.proc = nil
	r.lastErr = fmt.Sprintf("%s exited unexpectedly: %v", r.spec.Name, p.err)
	r.spec.Log.Error(r.lastErr)
	go func() {
		time.Sleep(5 * time.Second)
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.proc == nil && r.current != nil {
			if err := r.startLocked(configPath); err != nil {
				r.lastErr = "restart failed: " + err.Error()
				r.spec.Log.Error(r.lastErr)
			} else {
				r.lastErr = ""
				r.spec.Log.Info("restarted")
			}
		}
	}()
}

func (r *Runner) stopLocked() {
	p := r.proc
	if p == nil {
		return
	}
	r.proc = nil
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

func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
	r.current = nil
}

// Run executes the core binary once (version, test, API calls).
func (r *Runner) Run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.spec.Bin, args...)
	cmd.Env = append(os.Environ(), r.spec.Env...)
	return cmd.CombinedOutput()
}

func expand(args []string, configPath string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = strings.ReplaceAll(a, "{config}", configPath)
	}
	return out
}

// Tail shortens command output for error messages.
func Tail(out []byte, err error) string {
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

func (l *lineRing) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, s)
	if len(l.lines) > 5 {
		l.lines = l.lines[1:]
	}
}

func (l *lineRing) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, " | ")
}
