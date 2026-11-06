package sniproxy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

// clientHello — настоящий ClientHello из crypto/tls с заданным SNI.
func clientHello(t *testing.T, sni string) []byte {
	a, b := net.Pipe()
	go func() {
		c := tls.Client(a, &tls.Config{ServerName: sni, InsecureSkipVerify: true})
		c.SetDeadline(time.Now().Add(time.Second))
		c.Handshake()
		a.Close()
	}()
	b.SetDeadline(time.Now().Add(time.Second))
	got, sniOut := readHello(b)
	b.Close()
	if sniOut != sni {
		t.Fatalf("SNI = %q, want %q", sniOut, sni)
	}
	return got
}

func TestReadHello(t *testing.T) {
	for _, s := range []string{"www.apple.com", "www.microsoft.com"} {
		if h := clientHello(t, s); len(h) < 100 || h[0] != 0x16 {
			t.Fatalf("hello for %s looks wrong (%d bytes)", s, len(h))
		}
	}
}

// Маршрутизация: по SNI в нужное «ядро», остальное — по умолчанию с PROXY v1.
func TestRoute(t *testing.T) {
	backend := func() (string, chan string) {
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		got := make(chan string, 1)
		go func() {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.SetReadDeadline(time.Now().Add(2 * time.Second))
			b, _ := io.ReadAll(io.LimitReader(c, 64))
			got <- string(b)
			c.Close()
		}()
		return ln.Addr().String(), got
	}
	sbAddr, sbGot := backend()
	xrAddr, xrGot := backend()
	a := New(slog.Default())
	cfg, _ := json.Marshal(Config{Listeners: []Listener{{
		Listen:  "127.0.0.1:0",
		Routes:  []Route{{SNI: "www.apple.com", To: sbAddr}},
		Default: Route{To: xrAddr, ProxyProtocol: true},
	}}})
	if err := a.Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	defer a.Stop()
	front := a.lns[0].Addr().String()
	send := func(sni string) {
		c, err := net.Dial("tcp", front)
		if err != nil {
			t.Fatal(err)
		}
		tc := tls.Client(c, &tls.Config{ServerName: sni, InsecureSkipVerify: true})
		tc.SetDeadline(time.Now().Add(500 * time.Millisecond))
		tc.Handshake()
		c.Close()
	}
	send("www.apple.com")
	if s := <-sbGot; !strings.HasPrefix(s, "\x16\x03") {
		t.Fatalf("sing-box got %q, want a TLS record", s[:min(len(s), 10)])
	}
	send("www.microsoft.com")
	if s := <-xrGot; !strings.HasPrefix(s, "PROXY TCP4 127.0.0.1 127.0.0.1 ") {
		t.Fatalf("xray got %q, want PROXY header first", s[:min(len(s), 40)])
	}
}
