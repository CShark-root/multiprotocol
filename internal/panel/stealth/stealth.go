// Package stealth keeps the panel invisible to anyone who does not know its
// secret path: everything else gets a featureless response or a dropped
// connection, and plain-HTTP probes on the TLS port get no reply at all.
package stealth

import (
	"crypto/subtle"
	"io"
	"net"
	"net/http"
	"strings"

	"panel/internal/panel/config"
)

// Gate dispatches requests by their first path segment: /<secret>/... goes
// to routes[secret]; everything else is answered according to mode.
// Secrets are compared in constant time.
func Gate(mode config.DecoyMode, routes map[string]http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seg, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		var next http.Handler
		var secret string
		for s, h := range routes {
			if subtle.ConstantTimeCompare([]byte(seg), []byte(s)) == 1 {
				next, secret = h, s
			}
		}
		if next == nil {
			Decoy(w, mode)
			return
		}
		if prefix := "/" + secret + "/"; !strings.HasPrefix(r.URL.Path, prefix) {
			// Exactly "/<secret>": only someone who knows it gets here.
			http.Redirect(w, r, prefix, http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Decoy answers a request that is not meant for the panel.
func Decoy(w http.ResponseWriter, mode config.DecoyMode) {
	if mode == config.DecoyDrop {
		// Aborts the connection (HTTP/1) or the stream (HTTP/2) without
		// writing a single byte; net/http does not log this panic.
		panic(http.ErrAbortHandler)
	}
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusNotFound)
}

// TLSOnlyListener wraps a raw TCP listener so connections that do not start
// with a TLS handshake record are closed silently. Without it Go's server
// answers plain-HTTP probes with its recognisable "Client sent an HTTP
// request to an HTTPS server" reply.
func TLSOnlyListener(l net.Listener) net.Listener { return tlsOnlyListener{l} }

type tlsOnlyListener struct{ net.Listener }

func (l tlsOnlyListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &tlsOnlyConn{Conn: c}, nil
}

const recordTypeHandshake = 0x16

type tlsOnlyConn struct {
	net.Conn
	checked bool
}

func (c *tlsOnlyConn) Read(p []byte) (int, error) {
	if c.checked || len(p) == 0 {
		return c.Conn.Read(p)
	}
	n, err := c.Conn.Read(p[:1])
	if n == 0 {
		return n, err
	}
	c.checked = true
	if p[0] != recordTypeHandshake {
		c.Conn.Close()
		return 0, io.EOF
	}
	return n, err
}
