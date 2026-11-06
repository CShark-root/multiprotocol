// Package sniproxy делит один TCP-порт (443) между ядрами по имени сайта (SNI)
// из первого пакета TLS — как nginx ssl_preread или HAProxy. Ничего не
// расшифровывает: читает ClientHello, выбирает ядро и дальше просто
// перекладывает байты. Нужен, когда на 443 живут и Reality Xray, и Reality
// sing-box (для клиентов sing-box, которых новый Xray не пускает).
package sniproxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"panel/internal/agent"
)

// Config — что слушать и куда отправлять.
type Config struct {
	Listeners []Listener `json:"listeners"`
}

type Listener struct {
	Listen string  `json:"listen"` // ":443"
	Routes []Route `json:"routes"`
	// Default — куда всё остальное (чужие, сканеры, неизвестный SNI): Xray,
	// который показывает им настоящий сайт.
	Default Route `json:"default"`
}

type Route struct {
	SNI string `json:"sni,omitempty"`
	To  string `json:"to"` // 127.0.0.1:порт ядра
	// ProxyProtocol — передать ядру настоящий адрес клиента (PROXY v1).
	ProxyProtocol bool `json:"proxyProtocol,omitempty"`
}

type Adapter struct {
	log *slog.Logger

	mu      sync.Mutex
	current []byte
	cancel  context.CancelFunc
	lns     []net.Listener
	lastErr string
}

func New(log *slog.Logger) *Adapter { return &Adapter{log: log} }

func (a *Adapter) Name() string { return "sniproxy" }

// Apply запускает слушатели по конфигу (nil — останавливает).
func (a *Adapter) Apply(_ context.Context, cfg []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if bytes.Equal(cfg, a.current) && (cfg == nil || a.lns != nil) {
		return nil
	}
	a.stopLocked()
	a.current = cfg
	if cfg == nil {
		return nil
	}
	var c Config
	if err := json.Unmarshal(cfg, &c); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	for _, l := range c.Listeners {
		ln, err := net.Listen("tcp", l.Listen)
		if err != nil {
			a.stopLocked()
			a.lastErr = err.Error()
			return fmt.Errorf("listen %s: %w", l.Listen, err)
		}
		a.lns = append(a.lns, ln)
		go a.serve(ctx, ln, l)
	}
	a.lastErr = ""
	return nil
}

func (a *Adapter) stopLocked() {
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	for _, ln := range a.lns {
		ln.Close()
	}
	a.lns = nil
}

func (a *Adapter) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopLocked()
	a.current = nil
}

func (a *Adapter) Traffic(context.Context) ([]agent.TrafficDelta, error) { return nil, nil }
func (a *Adapter) Online(context.Context) ([]agent.OnlineEntry, error)   { return nil, nil }

func (a *Adapter) Health(context.Context) agent.HealthStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	// В списке ядер виден, только когда действительно делит порт.
	return agent.HealthStatus{Installed: a.current != nil, Running: len(a.lns) > 0, Version: "builtin", Message: a.lastErr}
}

func (a *Adapter) serve(ctx context.Context, ln net.Listener, l Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() == nil {
				a.log.Warn("sniproxy accept", "err", err)
			}
			return
		}
		go a.handle(ctx, c, l)
	}
}

func (a *Adapter) handle(ctx context.Context, c net.Conn, l Listener) {
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	hello, sni := readHello(c)
	c.SetReadDeadline(time.Time{})
	route := l.Default
	for _, r := range l.Routes {
		if sni != "" && strings.EqualFold(sni, r.SNI) {
			route = r
			break
		}
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	up, err := d.DialContext(ctx, "tcp", route.To)
	if err != nil {
		a.log.Warn("sniproxy dial", "to", route.To, "err", err)
		return
	}
	defer up.Close()
	if route.ProxyProtocol {
		if _, err := io.WriteString(up, proxyV1(c.RemoteAddr(), c.LocalAddr())); err != nil {
			return
		}
	}
	if _, err := up.Write(hello); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { io.Copy(up, c); closeWrite(up); done <- struct{}{} }()
	go func() { io.Copy(c, up); closeWrite(c); done <- struct{}{} }()
	<-done
	<-done
}

func closeWrite(c net.Conn) {
	if t, ok := c.(*net.TCPConn); ok {
		t.CloseWrite()
	}
}

// proxyV1 — заголовок PROXY protocol v1 с настоящим адресом клиента.
func proxyV1(src, dst net.Addr) string {
	s, okS := src.(*net.TCPAddr)
	d, okD := dst.(*net.TCPAddr)
	if !okS || !okD {
		return "PROXY UNKNOWN\r\n"
	}
	fam := "TCP4"
	if s.IP.To4() == nil {
		fam = "TCP6"
	}
	return fmt.Sprintf("PROXY %s %s %s %d %d\r\n", fam, s.IP.String(), d.IP.String(), s.Port, d.Port)
}

// readHello читает начало соединения, пока не наберётся весь ClientHello
// (он может занимать несколько TLS-записей), и достаёт из него SNI.
// Возвращает прочитанные байты — их надо отдать ядру как есть.
func readHello(r io.Reader) ([]byte, string) {
	var buf []byte
	var hs []byte // содержимое записей handshake
	for len(buf) < 64<<10 {
		head := make([]byte, 5)
		if _, err := io.ReadFull(r, head); err != nil {
			return append(buf, head...), ""
		}
		buf = append(buf, head...)
		if head[0] != 0x16 { // не TLS handshake
			return buf, ""
		}
		n := int(binary.BigEndian.Uint16(head[3:5]))
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			return append(buf, body...), ""
		}
		buf = append(buf, body...)
		hs = append(hs, body...)
		if len(hs) >= 4 {
			need := 4 + (int(hs[1])<<16 | int(hs[2])<<8 | int(hs[3]))
			if len(hs) >= need {
				sni, _ := parseSNI(hs[:need])
				return buf, sni
			}
		}
	}
	return buf, ""
}

var errShort = errors.New("short client hello")

// parseSNI достаёт server_name из сообщения ClientHello (с заголовком handshake).
func parseSNI(m []byte) (string, error) {
	if len(m) < 4 || m[0] != 1 {
		return "", errors.New("not a client hello")
	}
	p := m[4:]
	skip := func(n int) bool {
		if len(p) < n {
			return false
		}
		p = p[n:]
		return true
	}
	vec := func(lenBytes int) ([]byte, bool) {
		if len(p) < lenBytes {
			return nil, false
		}
		n := 0
		for i := 0; i < lenBytes; i++ {
			n = n<<8 | int(p[i])
		}
		p = p[lenBytes:]
		if len(p) < n {
			return nil, false
		}
		v := p[:n]
		p = p[n:]
		return v, true
	}
	if !skip(2 + 32) { // версия, random
		return "", errShort
	}
	if _, ok := vec(1); !ok { // session id
		return "", errShort
	}
	if _, ok := vec(2); !ok { // cipher suites
		return "", errShort
	}
	if _, ok := vec(1); !ok { // compression
		return "", errShort
	}
	exts, ok := vec(2)
	if !ok {
		return "", nil // без расширений — без SNI
	}
	for len(exts) >= 4 {
		typ := int(exts[0])<<8 | int(exts[1])
		n := int(exts[2])<<8 | int(exts[3])
		if len(exts) < 4+n {
			return "", errShort
		}
		data := exts[4 : 4+n]
		exts = exts[4+n:]
		if typ != 0 { // server_name
			continue
		}
		if len(data) < 2 {
			return "", errShort
		}
		list := data[2:]
		for len(list) >= 3 {
			kind := list[0]
			l := int(list[1])<<8 | int(list[2])
			if len(list) < 3+l {
				return "", errShort
			}
			if kind == 0 {
				return string(list[3 : 3+l]), nil
			}
			list = list[3+l:]
		}
	}
	return "", nil
}
