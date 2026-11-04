// Package amnezia reads the Amnezia containers that the official Amnezia
// app installed on the server. It never changes them: the helper (run as
// root by its own systemd unit, since Docker's socket is root-equivalent)
// answers a fixed set of read-only questions, and the panel, which stays
// unprivileged, asks it over a unix socket.
package amnezia

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const DockerSocket = "/var/run/docker.sock"

// Status is everything the Amnezia tab shows.
type Status struct {
	Docker     bool        `json:"docker"` // Docker answered at all
	Containers []Container `json:"containers"`
	CheckedAt  time.Time   `json:"checkedAt"`
}

type Container struct {
	Name      string    `json:"name"` // amnezia-awg2
	Kind      string    `json:"kind"` // awg2, xray, wireguard, openvpn...
	Image     string    `json:"image"`
	State     string    `json:"state"` // running, exited...
	Status    string    `json:"status"`
	StartedAt time.Time `json:"startedAt"`
	Ports     []string  `json:"ports"` // "51820/udp"
	CPU       float64   `json:"cpu"`   // percent of one core
	MemBytes  uint64    `json:"memBytes"`
	Clients   []Client  `json:"clients"`
	Note      string    `json:"note,omitempty"` // why clients are missing
}

// Client is one access shared from the Amnezia app. Keys stay out: only
// a short fingerprint of the public key is shown.
type Client struct {
	Name          string    `json:"name"`
	ID            string    `json:"id"` // shortened public key / client id
	Created       string    `json:"created,omitempty"`
	LastHandshake *time.Time `json:"lastHandshake,omitempty"` // nil: the protocol does not report it
	Endpoint      string    `json:"endpoint,omitempty"`
	RxBytes       int64     `json:"rxBytes"`
	TxBytes       int64     `json:"txBytes"`
}

// docker is a minimal client for the Docker Engine API over its socket.
type docker struct {
	http *http.Client
}

func newDocker(socket string) *docker {
	return &docker{http: &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}},
	}}
}

func (d *docker) do(ctx context.Context, method, path string, body any, out any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("docker %s %s: HTTP %d", method, strings.Split(path, "?")[0], resp.StatusCode)
	}
	if out != nil {
		return data, json.Unmarshal(data, out)
	}
	return data, nil
}

type dockerContainer struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	Image  string   `json:"Image"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
	Ports  []struct {
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
}

// list returns the Amnezia containers (name "amnezia-*"), running or not.
func (d *docker) list(ctx context.Context) ([]dockerContainer, error) {
	var all []dockerContainer
	q := url.Values{"all": {"1"}, "filters": {`{"name":["amnezia-"]}`}}
	if _, err := d.do(ctx, http.MethodGet, "/containers/json?"+q.Encode(), nil, &all); err != nil {
		return nil, err
	}
	out := all[:0]
	for _, c := range all {
		if len(c.Names) > 0 && strings.HasPrefix(strings.TrimPrefix(c.Names[0], "/"), "amnezia-") {
			out = append(out, c)
		}
	}
	return out, nil
}

// exec runs one of the fixed read-only commands below inside a container.
func (d *docker) exec(ctx context.Context, id string, cmd []string) (string, error) {
	var created struct {
		ID string `json:"Id"`
	}
	if _, err := d.do(ctx, http.MethodPost, "/containers/"+id+"/exec", map[string]any{
		"Cmd": cmd, "AttachStdout": true, "AttachStderr": false, "Tty": true,
	}, &created); err != nil {
		return "", err
	}
	out, err := d.do(ctx, http.MethodPost, "/exec/"+created.ID+"/start", map[string]any{"Detach": false, "Tty": true}, nil)
	return string(out), err
}

// The only commands the helper ever runs, all of them reads.
var (
	cmdShowAWG = []string{"sh", "-c", "awg show all dump 2>/dev/null || wg show all dump 2>/dev/null"}
	cmdClients = []string{"sh", "-c", "for f in /opt/amnezia/*/clientsTable; do [ -f \"$f\" ] && cat \"$f\" && echo; done"}
)

// Collect reads all Amnezia containers. Errors for one container end up in
// its Note instead of failing the whole status.
func Collect(ctx context.Context, socket string) Status {
	st := Status{CheckedAt: time.Now().UTC(), Containers: []Container{}}
	d := newDocker(socket)
	list, err := d.list(ctx)
	if err != nil {
		return st
	}
	st.Docker = true
	for _, dc := range list {
		name := strings.TrimPrefix(dc.Names[0], "/")
		c := Container{Name: name, Kind: strings.TrimPrefix(name, "amnezia-"), Image: dc.Image, State: dc.State, Status: dc.Status, Ports: []string{}, Clients: []Client{}}
		for _, p := range dc.Ports {
			// Docker lists a port once per address family.
			if port := strconv.Itoa(p.PublicPort) + "/" + p.Type; p.PublicPort != 0 && !slices.Contains(c.Ports, port) {
				c.Ports = append(c.Ports, port)
			}
		}
		var info struct {
			State struct {
				StartedAt time.Time `json:"StartedAt"`
			} `json:"State"`
		}
		if _, err := d.do(ctx, http.MethodGet, "/containers/"+dc.ID+"/json", nil, &info); err == nil {
			c.StartedAt = info.State.StartedAt
		}
		if dc.State == "running" {
			c.CPU, c.MemBytes = stats(ctx, d, dc.ID)
			c.Clients, c.Note = clients(ctx, d, dc.ID, c.Kind)
		}
		st.Containers = append(st.Containers, c)
	}
	return st
}

func stats(ctx context.Context, d *docker, id string) (float64, uint64) {
	var s struct {
		CPU struct {
			Usage struct {
				Total uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System uint64 `json:"system_cpu_usage"`
			Online int    `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPU struct {
			Usage struct {
				Total uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		Memory struct {
			Usage uint64            `json:"usage"`
			Stats map[string]uint64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if _, err := d.do(ctx, http.MethodGet, "/containers/"+id+"/stats?stream=false", nil, &s); err != nil {
		return 0, 0
	}
	var cpu float64
	if dc, ds := float64(s.CPU.Usage.Total)-float64(s.PreCPU.Usage.Total), float64(s.CPU.System)-float64(s.PreCPU.System); dc > 0 && ds > 0 {
		cpu = dc / ds * float64(max(1, s.CPU.Online)) * 100
	}
	mem := s.Memory.Usage
	if inactive, ok := s.Memory.Stats["inactive_file"]; ok && inactive < mem {
		mem -= inactive // what `docker stats` shows
	}
	return cpu, mem
}

// clients joins the names from clientsTable with live WireGuard-style data.
func clients(ctx context.Context, d *docker, id, kind string) ([]Client, string) {
	names := map[string]Client{}
	var order []string
	if out, err := d.exec(ctx, id, cmdClients); err == nil {
		for _, c := range parseClientsTable(out) {
			if _, dup := names[c.ID]; !dup {
				order = append(order, c.ID)
			}
			names[c.ID] = c
		}
	}
	live := map[string]Client{}
	if strings.HasPrefix(kind, "awg") || kind == "wireguard" {
		if out, err := d.exec(ctx, id, cmdShowAWG); err == nil {
			for _, p := range parseDump(out) {
				live[p.ID] = p
				if _, ok := names[p.ID]; !ok {
					order = append(order, p.ID)
				}
			}
		}
	}
	list := make([]Client, 0, len(order))
	for _, cid := range order {
		c, named := names[cid]
		if l, ok := live[cid]; ok {
			c.LastHandshake, c.Endpoint, c.RxBytes, c.TxBytes = l.LastHandshake, l.Endpoint, l.RxBytes, l.TxBytes
		}
		if !named {
			c.Name = ""
		}
		c.ID = short(cid)
		list = append(list, c)
	}
	note := ""
	if len(list) == 0 {
		note = "no clients found"
	}
	return list, note
}

func short(id string) string {
	if len(id) > 10 {
		return id[:6] + "…" + id[len(id)-4:]
	}
	return id
}

// parseClientsTable accepts the current format (an array of
// {clientId, userData:{clientName, creationDate}}) and the older map
// {clientId: {clientName}}, possibly several files back to back.
func parseClientsTable(out string) []Client {
	var list []Client
	dec := json.NewDecoder(strings.NewReader(out))
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			break
		}
		var arr []struct {
			ClientID string `json:"clientId"`
			UserData struct {
				ClientName   string `json:"clientName"`
				CreationDate string `json:"creationDate"`
			} `json:"userData"`
		}
		if json.Unmarshal(raw, &arr) == nil {
			for _, a := range arr {
				if a.ClientID != "" {
					list = append(list, Client{ID: a.ClientID, Name: a.UserData.ClientName, Created: a.UserData.CreationDate})
				}
			}
			continue
		}
		var obj map[string]struct {
			ClientName string `json:"clientName"`
		}
		if json.Unmarshal(raw, &obj) == nil {
			for id, v := range obj {
				list = append(list, Client{ID: id, Name: v.ClientName})
			}
		}
	}
	return list
}

// parseDump reads `awg show all dump`. The first line of each interface
// carries its private key and is skipped; peer lines are
// iface pubkey psk endpoint allowed-ips handshake rx tx keepalive.
// Only the public key, endpoint, handshake and counters are kept.
func parseDump(out string) []Client {
	var peers []Client
	seen := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Split(strings.TrimRight(sc.Text(), "\r"), "\t")
		if len(f) < 2 {
			continue
		}
		if !seen[f[0]] {
			seen[f[0]] = true // interface line: holds the private key
			continue
		}
		if len(f) < 8 {
			continue
		}
		p := Client{ID: f[1]}
		if f[3] != "(none)" {
			p.Endpoint = f[3]
		}
		// 0 = never; kept as the zero time so the UI can say so.
		ts, _ := strconv.ParseInt(f[5], 10, 64)
		hs := time.Time{}
		if ts > 0 {
			hs = time.Unix(ts, 0).UTC()
		}
		p.LastHandshake = &hs
		p.RxBytes, _ = strconv.ParseInt(f[6], 10, 64)
		p.TxBytes, _ = strconv.ParseInt(f[7], 10, 64)
		peers = append(peers, p)
	}
	return peers
}

// Logs returns the last lines a container printed. Many Amnezia containers
// run with logging off; then there is nothing to show.
func Logs(ctx context.Context, socket, name string, tail int) ([]string, error) {
	d := newDocker(socket)
	list, err := d.list(ctx)
	if err != nil {
		return nil, err
	}
	var id string
	for _, c := range list {
		if strings.TrimPrefix(c.Names[0], "/") == name {
			id = c.ID
		}
	}
	if id == "" {
		return nil, errors.New("no such Amnezia container")
	}
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}, "tail": {strconv.Itoa(tail)}}
	raw, err := d.do(ctx, http.MethodGet, "/containers/"+id+"/logs?"+q.Encode(), nil, nil)
	if err != nil {
		return nil, err
	}
	return demux(raw), nil
}

// demux splits Docker's multiplexed log stream (8-byte frame headers) into
// lines; a TTY container sends plain text instead.
func demux(raw []byte) []string {
	var text bytes.Buffer
	b := raw
	framed := len(b) >= 8 && (b[0] == 1 || b[0] == 2) && b[1] == 0 && b[2] == 0 && b[3] == 0
	for framed && len(b) >= 8 {
		n := int(binary.BigEndian.Uint32(b[4:8]))
		if len(b) < 8+n {
			break
		}
		text.Write(b[8 : 8+n])
		b = b[8+n:]
	}
	if !framed {
		text.Write(raw)
	}
	lines := strings.Split(strings.TrimRight(text.String(), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}
	}
	return lines
}
