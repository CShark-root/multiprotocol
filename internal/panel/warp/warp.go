// Package warp registers a free Cloudflare WARP device (the way wgcf
// does) and checks a connection through it. The cores then reach WARP as an
// ordinary WireGuard peer in user space: no system interface is created, so
// the host network, Docker and Amnezia are left alone.
package warp

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"panel/internal/panel/routing"
)

const api = "https://api.cloudflareclient.com/v0a2158"

var client = &http.Client{Timeout: 20 * time.Second}

type regResponse struct {
	ID      string `json:"id"`
	Token   string `json:"token"`
	Account struct {
		License     string `json:"license"`
		AccountType string `json:"account_type"`
		WarpPlus    bool   `json:"warp_plus"`
	} `json:"account"`
	Config struct {
		ClientID string `json:"client_id"`
		Peers    []struct {
			PublicKey string `json:"public_key"`
			Endpoint  struct {
				Host string `json:"host"`
				V4   string `json:"v4"`
			} `json:"endpoint"`
		} `json:"peers"`
		Interface struct {
			Addresses struct {
				V4 string `json:"v4"`
				V6 string `json:"v6"`
			} `json:"addresses"`
		} `json:"interface"`
	} `json:"config"`
}

func call(ctx context.Context, method, path, token string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, api+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "okhttp/3.12.1")
	req.Header.Set("CF-Client-Version", "a-6.11-2223")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("cloudflare: HTTP %d %s", resp.StatusCode, msg)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Register creates a new WARP device with a fresh key pair.
func Register(ctx context.Context) (*routing.WarpAccount, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"key":        base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()),
		"install_id": "",
		"fcm_token":  "",
		"tos":        time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		"model":      "PC",
		"type":       "Android",
		"locale":     "en_US",
	}
	var r regResponse
	if err := call(ctx, http.MethodPost, "/reg", "", body, &r); err != nil {
		return nil, err
	}
	if len(r.Config.Peers) == 0 || r.Config.Interface.Addresses.V4 == "" {
		return nil, errors.New("cloudflare: unexpected registration response")
	}
	acc := &routing.WarpAccount{
		ID: r.ID, Token: r.Token, License: r.Account.License, WarpPlus: isPlus(r),
		PrivateKey:    base64.StdEncoding.EncodeToString(key.Bytes()),
		PeerPublicKey: r.Config.Peers[0].PublicKey,
		Endpoint:      endpoint(r.Config.Peers[0].Endpoint.V4),
		Address4:      r.Config.Interface.Addresses.V4 + "/32",
		RegisteredAt:  time.Now().UTC(),
	}
	if v6 := r.Config.Interface.Addresses.V6; v6 != "" {
		acc.Address6 = v6 + "/128"
	}
	// client_id carries the three "reserved" bytes WARP expects in every
	// WireGuard packet header.
	if id, err := base64.StdEncoding.DecodeString(r.Config.ClientID); err == nil && len(id) == 3 {
		acc.Reserved = []int{int(id[0]), int(id[1]), int(id[2])}
	} else {
		acc.Reserved = []int{0, 0, 0}
	}
	return acc, nil
}

// endpoint is the IPv4 address of the peer on WireGuard port 2408 (the API
// reports port 0). The hostname is avoided on purpose: it resolves to IPv6
// first, and many servers have no IPv6 route.
func endpoint(v4 string) string {
	h := v4
	if hh, _, err := net.SplitHostPort(v4); err == nil {
		h = hh
	}
	if net.ParseIP(h).To4() == nil {
		h = "162.159.192.1"
	}
	return net.JoinHostPort(h, "2408")
}

// isPlus: free devices report account_type "free" (and, confusingly,
// warp_plus true).
func isPlus(r regResponse) bool {
	return r.Account.AccountType != "" && r.Account.AccountType != "free"
}

// SetLicense applies a WARP+ key to the device and reports whether the
// account is WARP+ now.
func SetLicense(ctx context.Context, acc *routing.WarpAccount, license string) (bool, error) {
	if err := call(ctx, http.MethodPut, "/reg/"+acc.ID+"/account", acc.Token, map[string]string{"license": license}, nil); err != nil {
		return false, err
	}
	var r regResponse
	if err := call(ctx, http.MethodGet, "/reg/"+acc.ID, acc.Token, nil, &r); err != nil {
		return false, err
	}
	return isPlus(r), nil
}

// Delete removes the device from Cloudflare (best effort).
func Delete(ctx context.Context, acc *routing.WarpAccount) error {
	return call(ctx, http.MethodDelete, "/reg/"+acc.ID, acc.Token, nil, nil)
}

// Trace is what https://www.cloudflare.com/cdn-cgi/trace reports.
type Trace struct {
	IP        string `json:"ip"`
	Country   string `json:"country"`
	Warp      string `json:"warp"` // "on", "plus" or "off"
	LatencyMs int64  `json:"latencyMs"`
}

// Check fetches Cloudflare's trace page through the SOCKS bridge into
// Xray's WARP outbound (or directly when bridge is nil).
func Check(ctx context.Context, bridge *routing.Bridge) (Trace, error) {
	tr := &http.Transport{DisableKeepAlives: true}
	if bridge != nil {
		b := *bridge
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialSOCKS(ctx, "127.0.0.1:"+strconv.Itoa(b.Port), b.User, b.Password, addr)
		}
	}
	c := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.cloudflare.com/cdn-cgi/trace", nil)
	start := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		return Trace{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	t := Trace{LatencyMs: time.Since(start).Milliseconds()}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, _ := strings.Cut(line, "=")
		switch k {
		case "ip":
			t.IP = v
		case "loc":
			t.Country = v
		case "warp":
			t.Warp = v
		}
	}
	if t.IP == "" {
		return t, errors.New("unexpected trace response")
	}
	return t, nil
}

// dialSOCKS opens a TCP connection to addr through a SOCKS5 proxy with
// username/password auth (RFC 1928, RFC 1929).
func dialSOCKS(ctx context.Context, proxy, user, pass, addr string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", proxy)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	} else {
		conn.SetDeadline(time.Now().Add(15 * time.Second))
	}
	fail := func(err error) (net.Conn, error) { conn.Close(); return nil, err }
	buf := make([]byte, 0, 300)
	if _, err := conn.Write([]byte{5, 1, 2}); err != nil {
		return fail(err)
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil || resp[1] != 2 {
		return fail(errors.New("socks: password auth refused"))
	}
	buf = append(buf, 1, byte(len(user)))
	buf = append(buf, user...)
	buf = append(buf, byte(len(pass)))
	buf = append(buf, pass...)
	if _, err := conn.Write(buf); err != nil {
		return fail(err)
	}
	if _, err := io.ReadFull(conn, resp); err != nil || resp[1] != 0 {
		return fail(errors.New("socks: bad credentials"))
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fail(err)
	}
	port, _ := strconv.Atoi(portStr)
	req := []byte{5, 1, 0, 3, byte(len(host))}
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	if _, err := conn.Write(req); err != nil {
		return fail(err)
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fail(err)
	}
	if head[1] != 0 {
		return fail(fmt.Errorf("socks: connect failed (%d)", head[1]))
	}
	skip := 0
	switch head[3] {
	case 1:
		skip = 4
	case 4:
		skip = 16
	case 3:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return fail(err)
		}
		skip = int(l[0])
	}
	if _, err := io.ReadFull(conn, make([]byte, skip+2)); err != nil {
		return fail(err)
	}
	conn.SetDeadline(time.Time{})
	return conn, nil
}

// Ports — порты WireGuard, на которых отвечает WARP. Если провайдер VPS
// режет один, сторож переходит на следующий.
var Ports = []string{"2408", "500", "1701", "4500", "854", "4233"}

// NextEndpoint возвращает тот же адрес на следующем порту из Ports.
func NextEndpoint(ep string) string {
	h, p, err := net.SplitHostPort(ep)
	if err != nil {
		return endpoint(ep)
	}
	i := slices.Index(Ports, p)
	return net.JoinHostPort(h, Ports[(i+1)%len(Ports)])
}
