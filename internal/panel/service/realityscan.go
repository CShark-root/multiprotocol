package service

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"panel/internal/panel/validate"
)

// RealityCandidates are well-known sites that usually serve TLS 1.3 with
// HTTP/2 from many locations. The scan measures them from this server.
var RealityCandidates = []string{
	"www.microsoft.com", "www.apple.com", "dl.google.com", "www.samsung.com", "www.nvidia.com",
	"addons.mozilla.org", "www.amd.com", "www.cisco.com", "www.intel.com", "www.oracle.com",
	"www.dell.com", "www.hp.com", "www.lenovo.com", "www.asus.com", "www.logitech.com",
	"www.sony.com", "swdist.apple.com", "www.bing.com", "www.yahoo.com", "www.tesla.com",
}

// TargetResult is how well a site suits Reality (or ShadowTLS) as seen from
// this server.
type TargetResult struct {
	Host   string `json:"host"`
	IP     string `json:"ip,omitempty"`
	Good   bool   `json:"good"`   // TLS 1.3 + HTTP/2 + valid certificate
	TLS13  bool   `json:"tls13"`  // required by Reality
	H2     bool   `json:"h2"`     // what real browsers negotiate; XHTTP/gRPC need it
	CertOK bool   `json:"certOk"` // a valid certificate for that name
	RTTms  int    `json:"rttMs"`  // median TLS handshake time; lower = closer
	Error  string `json:"error,omitempty"`
}

type Scanner struct {
	lookup    func(ctx context.Context, host string) ([]net.IPAddr, error)
	handshake func(ctx context.Context, ip, host string) (tls.ConnectionState, time.Duration, error)
}

func NewScanner() *Scanner {
	return &Scanner{lookup: net.DefaultResolver.LookupIPAddr, handshake: tlsHandshake}
}

// Scan checks the hosts (RealityCandidates when empty) in parallel and
// returns them best first: good ones by latency, then the rest.
func (s *Scanner) Scan(ctx context.Context, hosts []string) ([]TargetResult, error) {
	if len(hosts) == 0 {
		hosts = RealityCandidates
	}
	if len(hosts) > 50 {
		return nil, validate.Fail("hosts", "at most 50 hosts")
	}
	for i, h := range hosts {
		hosts[i] = strings.ToLower(strings.TrimSpace(h))
		if !hostnameRe.MatchString(hosts[i]) {
			return nil, validate.Fail("hosts", "domain names only: "+h)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	results := make([]TargetResult, len(hosts))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = s.check(ctx, h)
		}()
	}
	wg.Wait()
	rank(results)
	return results, nil
}

func (s *Scanner) check(ctx context.Context, host string) TargetResult {
	r := TargetResult{Host: host}
	lctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	addrs, err := s.lookup(lctx, host)
	cancel()
	if err != nil || len(addrs) == 0 {
		r.Error = "DNS: no address"
		return r
	}
	var ip net.IP
	for _, a := range addrs {
		// The scan must never become a way to probe the server's own network.
		if a.IP.IsPrivate() || a.IP.IsLoopback() || a.IP.IsLinkLocalUnicast() || a.IP.IsUnspecified() {
			r.Error = "resolves to a private address"
			return r
		}
		if ip == nil || (ip.To4() == nil && a.IP.To4() != nil) {
			ip = a.IP // prefer IPv4: most servers dial out over it
		}
	}
	r.IP = ip.String()

	var rtts []time.Duration
	for range 3 {
		st, d, err := s.handshake(ctx, r.IP, host)
		if err != nil {
			var verr *tls.CertificateVerificationError
			switch {
			case errors.As(err, &verr):
				r.Error = "certificate is not valid for this name"
			case strings.Contains(err.Error(), "protocol version"):
				r.Error = "no TLS 1.3"
			default:
				r.Error = "TLS handshake failed"
			}
			return r
		}
		r.TLS13, r.CertOK = st.Version == tls.VersionTLS13, true
		r.H2 = st.NegotiatedProtocol == "h2"
		rtts = append(rtts, d)
	}
	slices.Sort(rtts)
	r.RTTms = int(rtts[len(rtts)/2].Milliseconds())
	r.Good = r.TLS13 && r.H2 && r.CertOK
	if !r.H2 {
		r.Error = "no HTTP/2"
	}
	return r
}

func tlsHandshake(ctx context.Context, ip, host string) (tls.ConnectionState, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	start := time.Now()
	d := tls.Dialer{Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS13, NextProtos: []string{"h2", "http/1.1"}}}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, "443"))
	if err != nil {
		return tls.ConnectionState{}, 0, err
	}
	elapsed := time.Since(start)
	defer conn.Close()
	return conn.(*tls.Conn).ConnectionState(), elapsed, nil
}

func rank(rs []TargetResult) {
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Good != rs[j].Good {
			return rs[i].Good
		}
		if rs[i].Good {
			return rs[i].RTTms < rs[j].RTTms
		}
		return rs[i].Error == "" && rs[j].Error != ""
	})
}
