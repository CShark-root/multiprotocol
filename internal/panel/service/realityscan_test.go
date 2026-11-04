package service

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"testing"
	"time"
)

func TestScanRanksAndRefusesPrivateTargets(t *testing.T) {
	s := &Scanner{
		lookup: func(_ context.Context, host string) ([]net.IPAddr, error) {
			switch host {
			case "inside.example.com":
				return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil
			case "gone.example.com":
				return nil, errors.New("nxdomain")
			}
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
		},
		handshake: func(_ context.Context, ip, host string) (tls.ConnectionState, time.Duration, error) {
			switch host {
			case "fast.example.com":
				return tls.ConnectionState{Version: tls.VersionTLS13, NegotiatedProtocol: "h2"}, 10 * time.Millisecond, nil
			case "slow.example.com":
				return tls.ConnectionState{Version: tls.VersionTLS13, NegotiatedProtocol: "h2"}, 90 * time.Millisecond, nil
			case "http1.example.com":
				return tls.ConnectionState{Version: tls.VersionTLS13, NegotiatedProtocol: "http/1.1"}, 5 * time.Millisecond, nil
			}
			return tls.ConnectionState{}, 0, errors.New("tls: protocol version not supported")
		},
	}
	got, err := s.Scan(context.Background(), []string{
		"slow.example.com", "inside.example.com", "http1.example.com", "fast.example.com", "gone.example.com", "old.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Host != "fast.example.com" || got[1].Host != "slow.example.com" || !got[0].Good || got[0].RTTms != 10 {
		t.Fatalf("good targets must come first by latency: %+v", got[:2])
	}
	byHost := map[string]TargetResult{}
	for _, r := range got {
		byHost[r.Host] = r
	}
	if r := byHost["inside.example.com"]; r.Good || r.Error != "resolves to a private address" {
		t.Errorf("private target: %+v", r)
	}
	if r := byHost["http1.example.com"]; r.Good || r.H2 {
		t.Errorf("no HTTP/2 must not be good: %+v", r)
	}
	if r := byHost["old.example.com"]; r.Error != "no TLS 1.3" {
		t.Errorf("TLS 1.2 site: %+v", r)
	}
	if _, err := s.Scan(context.Background(), []string{"not a host"}); err == nil {
		t.Error("invalid host names must be rejected")
	}
}
