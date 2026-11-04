package amnezia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// HelperSocket is where the helper listens; the panel user may connect.
const HelperSocket = "/run/panel-amnezia/helper.sock"

var nameRe = regexp.MustCompile(`^amnezia-[a-z0-9-]{1,40}$`)

// Serve answers the panel's two questions, GET /status and
// GET /logs?name=amnezia-x&tail=N, on a unix socket that only root and the
// group gid can open. There is no way to send Docker anything else.
func Serve(ctx context.Context, socket, dockerSocket string, gid int, log *slog.Logger) error {
	os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	if err := os.Chmod(socket, 0o660); err != nil {
		return err
	}
	if gid >= 0 {
		if err := os.Chown(socket, 0, gid); err != nil {
			return err
		}
	}
	var mu sync.Mutex
	var cached Status
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		// Several panel tabs polling must not turn into a docker exec storm.
		if time.Since(cached.CheckedAt) > 5*time.Second {
			c, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			cached = Collect(c, dockerSocket)
			cancel()
		}
		writeJSON(w, cached)
	})
	mux.HandleFunc("GET /logs", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		if !nameRe.MatchString(name) {
			http.Error(w, "bad name", http.StatusBadRequest)
			return
		}
		tail = min(max(tail, 1), 1000)
		lines, err := Logs(r.Context(), dockerSocket, name, tail)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, lines)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	log.Info("amnezia helper listening", "socket", socket)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// HelperClient is the panel's side of the helper.
type HelperClient struct {
	http *http.Client
}

func NewClient(socket string) *HelperClient {
	return &HelperClient{http: &http.Client{
		Timeout: 40 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}},
	}}
}

// ErrNoHelper means the helper service is not installed or not running.
var ErrNoHelper = errors.New("amnezia helper is not running")

func (c *HelperClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://helper"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		var ne *net.OpError
		if errors.As(err, &ne) {
			return ErrNoHelper
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("amnezia helper: %s", string(b))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

func (c *HelperClient) Status(ctx context.Context) (Status, error) {
	var s Status
	err := c.get(ctx, "/status", &s)
	return s, err
}

func (c *HelperClient) Logs(ctx context.Context, name string, tail int) ([]string, error) {
	if !nameRe.MatchString(name) {
		return nil, errors.New("bad container name")
	}
	var lines []string
	err := c.get(ctx, "/logs?"+url.Values{"name": {name}, "tail": {strconv.Itoa(tail)}}.Encode(), &lines)
	return lines, err
}
