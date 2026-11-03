package stealth

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/panel/config"
	"panel/internal/panel/tlsutil"
)

const base = "S3cretPath_abc123"

func gated(mode config.DecoyMode) http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "panel") })
	return Gate(base, mode, ok)
}

func TestGateNotFoundMode(t *testing.T) {
	h := gated(config.DecoyNotFound)
	cases := []struct {
		path   string
		status int
		body   string
	}{
		{"/", http.StatusNotFound, ""},
		{"/admin", http.StatusNotFound, ""},
		{"/S3cretPath_abc12/", http.StatusNotFound, ""},
		{"/S3cretPath_abc1234/", http.StatusNotFound, ""},
		{"/" + base, http.StatusFound, ""},
		{"/" + base + "/", http.StatusOK, "panel"},
		{"/" + base + "/api/health", http.StatusOK, "panel"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d", c.path, rec.Code, c.status)
		}
		if c.status != http.StatusFound && rec.Body.String() != c.body {
			t.Errorf("%s: body %q, want %q", c.path, rec.Body.String(), c.body)
		}
	}
}

func TestGateDropModeWritesNothing(t *testing.T) {
	srv := httptest.NewServer(gated(config.DecoyDrop))
	defer srv.Close()

	if _, err := http.Get(srv.URL + "/wp-login.php"); err == nil {
		t.Fatal("expected the connection to be dropped")
	}
	resp, err := http.Get(srv.URL + "/" + base + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("panel path: status %d", resp.StatusCode)
	}
}

func TestTLSOnlyListenerIgnoresPlainHTTP(t *testing.T) {
	dir := t.TempDir()
	cert, err := tlsutil.EnsureSelfSigned(filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := tls.NewListener(TLSOnlyListener(raw), &tls.Config{Certificates: []tls.Certificate{cert}})
	srv := &http.Server{Handler: gated(config.DecoyNotFound), ErrorLog: nil}
	go srv.Serve(ln)
	defer srv.Close()

	conn, err := net.Dial("tcp", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	io.WriteString(conn, "GET / HTTP/1.1\r\nHost: x\r\n\r\n")
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	reply, _ := io.ReadAll(conn)
	if len(reply) != 0 {
		t.Fatalf("plain HTTP probe got a reply: %q", reply)
	}

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	resp, err := client.Get("https://" + raw.Addr().String() + "/" + base + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("TLS request: status %d", resp.StatusCode)
	}
}
