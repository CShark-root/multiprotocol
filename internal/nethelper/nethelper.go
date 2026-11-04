// Package nethelper keeps the one firewall table the panel owns: UDP port
// hopping for Hysteria2 (a range of ports redirected to the inbound's real
// port). Writing nftables needs root, so a tiny helper service (root,
// CAP_NET_ADMIN only) does it on the panel's request over a unix socket.
// It only ever replaces the table "inet panel_hop"; Docker's, Amnezia's
// and everyone else's rules are never read or touched.
package nethelper

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	Socket = "/run/panel-net/helper.sock"
	table  = "panel_hop"
)

// Hop redirects UDP ports From..To to Target (a Hysteria2 inbound).
type Hop struct {
	From   int `json:"from"`
	To     int `json:"to"`
	Target int `json:"target"`
}

func (h Hop) valid() error {
	if h.From < 1024 || h.To > 65535 || h.From > h.To || h.Target < 1 || h.Target > 65535 {
		return fmt.Errorf("bad range %d-%d -> %d", h.From, h.To, h.Target)
	}
	if h.To-h.From > 40000 {
		return fmt.Errorf("range %d-%d is too wide", h.From, h.To)
	}
	return nil
}

// ruleset is the nft script that atomically replaces our table (an empty
// list removes it).
func ruleset(hops []Hop) string {
	var b strings.Builder
	// "table …; delete table …" makes deletion safe when it does not exist.
	b.WriteString("table inet " + table + "\ndelete table inet " + table + "\n")
	if len(hops) == 0 {
		return b.String()
	}
	b.WriteString("table inet " + table + " {\n\tchain prerouting {\n\t\ttype nat hook prerouting priority dstnat; policy accept;\n")
	for _, h := range hops {
		fmt.Fprintf(&b, "\t\tudp dport %d-%d redirect to :%d\n", h.From, h.To, h.Target)
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

// listeningUDP lists UDP ports something is bound to (Amnezia's
// docker-proxy among them), from /proc.
func listeningUDP() map[int]bool {
	ports := map[int]bool{}
	for _, f := range []string{"/proc/net/udp", "/proc/net/udp6"} {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Scan() // header
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 4 {
				continue
			}
			_, portHex, ok := strings.Cut(fields[1], ":")
			// A bound service has no peer; connected sockets (outgoing
			// DNS, WireGuard) are not listeners.
			if !ok || strings.Trim(fields[2], "0:") != "" {
				continue
			}
			if p, err := strconv.ParseUint(portHex, 16, 16); err == nil {
				ports[int(p)] = true
			}
		}
	}
	return ports
}

// ephemeralRange is where the kernel picks source ports for outgoing
// connections; a redirect there could catch the server's own traffic.
func ephemeralRange() (int, int) {
	b, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err == nil {
		if f := strings.Fields(string(b)); len(f) == 2 {
			lo, err1 := strconv.Atoi(f[0])
			hi, err2 := strconv.Atoi(f[1])
			if err1 == nil && err2 == nil {
				return lo, hi
			}
		}
	}
	return 32768, 60999
}

// check refuses ranges that would capture another service's port, the
// kernel's ephemeral ports, or each other.
func check(hops []Hop, inUse map[int]bool) error {
	lo, hi := ephemeralRange()
	for i, h := range hops {
		if err := h.valid(); err != nil {
			return err
		}
		if h.From <= hi && lo <= h.To {
			return fmt.Errorf("range %d-%d overlaps the system's outgoing ports %d-%d; pick one below %d", h.From, h.To, lo, hi, lo)
		}
		for p := range inUse {
			if p >= h.From && p <= h.To && p != h.Target {
				return fmt.Errorf("range %d-%d includes port %d, which is in use", h.From, h.To, p)
			}
		}
		for _, o := range hops[:i] {
			if h.From <= o.To && o.From <= h.To {
				return fmt.Errorf("ranges %d-%d and %d-%d overlap", h.From, h.To, o.From, o.To)
			}
		}
	}
	return nil
}

func apply(ctx context.Context, hops []Hop) error {
	if _, err := exec.LookPath("nft"); err != nil {
		return errors.New("nft is not installed (apt install nftables)")
	}
	cmd := exec.CommandContext(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(ruleset(hops))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Serve runs the helper: PUT /hops replaces the whole list.
func Serve(ctx context.Context, socket string, gid int, log *slog.Logger) error {
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
	var current []Hop
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /hops", func(w http.ResponseWriter, r *http.Request) {
		var hops []Hop
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&hops); err != nil || len(hops) > 50 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		slices.SortFunc(hops, func(a, b Hop) int { return a.From - b.From })
		mu.Lock()
		defer mu.Unlock()
		if slices.Equal(hops, current) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err := check(hops, listeningUDP()); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		c, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := apply(c, hops); err != nil {
			log.Error("port hopping", "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		current = hops
		log.Info("port hopping applied", "ranges", len(hops))
		w.WriteHeader(http.StatusNoContent)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		srv.Close()
		// Leave no redirect behind when the helper stops for good.
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		apply(c, nil)
	}()
	log.Info("net helper listening", "socket", socket)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Client is the panel's side.
type Client struct{ http *http.Client }

func NewClient(socket string) *Client {
	return &Client{http: &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}},
	}}
}

var ErrNoHelper = errors.New("the network helper is not running")

// SetHops replaces the port hopping rules.
func (c *Client) SetHops(ctx context.Context, hops []Hop) error {
	if hops == nil {
		hops = []Hop{}
	}
	body, _ := json.Marshal(hops)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, "http://helper/hops", bytes.NewReader(body))
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
