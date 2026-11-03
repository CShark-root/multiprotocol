package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"panel/internal/agent"
	"panel/internal/panel/api"
	"panel/internal/panel/auth"
	"panel/internal/panel/service"
	"panel/internal/panel/store"
)

const base = "TestBasePath123"

type client struct {
	t    *testing.T
	url  string
	http *http.Client
}

func newClient(t *testing.T) *client {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	h := api.New(api.Deps{
		BasePath: base,
		Auth:     auth.NewService(st),
		Users:    service.NewUsers(st),
		Agent:    agent.New(),
		Web: fstest.MapFS{
			"index.html":       {Data: []byte("<html><head><title>x</title></head></html>")},
			"assets/app-1.js": {Data: []byte("console.log(1)")},
		},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &client{t: t, url: srv.URL + "/" + base, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path, contentType string, body any) (int, []byte) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.url+path, r)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func (c *client) json(method, path string, body any) (int, []byte) {
	return c.do(method, path, "application/json", body)
}

func (c *client) expect(status int, method, path string, body any) map[string]any {
	c.t.Helper()
	got, out := c.json(method, path, body)
	if got != status {
		c.t.Fatalf("%s %s: status %d, want %d: %s", method, path, got, status, out)
	}
	var m map[string]any
	json.Unmarshal(out, &m)
	return m
}

func TestAuthAndUsersFlow(t *testing.T) {
	c := newClient(t)

	if st := c.expect(200, "GET", "/api/auth/state", nil); st["setupRequired"] != true {
		t.Fatalf("fresh panel must require setup: %v", st)
	}
	c.expect(401, "GET", "/api/users", nil)

	weak := c.expect(400, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "short"})
	if weak["error"].(map[string]any)["field"] != "password" {
		t.Fatalf("expected password validation error: %v", weak)
	}
	c.expect(400, "POST", "/api/auth/setup", map[string]any{"username": "admin", "password": "x", "extra": 1})

	creds := map[string]string{"username": "admin", "password": "a-long-enough-password"}
	c.expect(200, "POST", "/api/auth/setup", creds)
	c.expect(409, "POST", "/api/auth/setup", creds)
	if st := c.expect(200, "GET", "/api/auth/state", nil); st["authenticated"] != true {
		t.Fatalf("setup must log the admin in: %v", st)
	}

	u := c.expect(201, "POST", "/api/users", map[string]any{"name": "phone", "trafficLimit": 1 << 30})
	if u["status"] != "active" || u["subToken"] == "" {
		t.Fatalf("unexpected new user: %v", u)
	}
	c.expect(409, "POST", "/api/users", map[string]any{"name": "PHONE"})
	c.expect(400, "POST", "/api/users", map[string]any{"name": "bad name; DROP TABLE users"})

	id := int(u["id"].(float64))
	path := "/api/users/" + itoa(id)
	upd := c.expect(200, "PUT", path, map[string]any{"name": "phone", "enabled": false})
	if upd["status"] != "disabled" {
		t.Fatalf("expected disabled status: %v", upd)
	}
	past := c.expect(200, "PUT", path, map[string]any{"name": "phone", "expireAt": "2000-01-01T00:00:00Z"})
	if past["status"] != "expired" {
		t.Fatalf("expected expired status: %v", past)
	}

	if code, body := c.json("GET", "/api/users", nil); code != 200 || !strings.Contains(string(body), `"phone"`) {
		t.Fatalf("list users: %d %s", code, body)
	}
	c.expect(204, "DELETE", path, nil)
	c.expect(404, "GET", path, nil)

	c.expect(204, "POST", "/api/auth/logout", nil)
	c.expect(401, "GET", "/api/users", nil)
	c.expect(401, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrong-password"})
	c.expect(401, "POST", "/api/auth/login", map[string]string{"username": "nobody", "password": "wrong-password"})
	c.expect(200, "POST", "/api/auth/login", creds)
	c.expect(200, "GET", "/api/system", nil)
}

func TestWritesRequireJSON(t *testing.T) {
	c := newClient(t)
	if code, _ := c.do("POST", "/api/auth/setup", "application/x-www-form-urlencoded", nil); code != 415 {
		t.Fatalf("form POST must be rejected, got %d", code)
	}
	req, _ := http.NewRequest("POST", c.url+"/api/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("cross-origin POST must be rejected, got %d", resp.StatusCode)
	}
}

func TestSPAServing(t *testing.T) {
	c := newClient(t)
	for _, p := range []string{"/", "/users", "/settings/security"} {
		code, body := c.do("GET", p, "", nil)
		if code != 200 || !strings.Contains(string(body), `<base href="/`+base+`/">`) {
			t.Fatalf("GET %s: %d %s", p, code, body)
		}
	}
	if code, _ := c.do("GET", "/assets/app-1.js", "", nil); code != 200 {
		t.Fatalf("asset: %d", code)
	}
	if code, _ := c.do("GET", "/assets/missing.js", "", nil); code != 404 {
		t.Fatalf("missing asset must 404, got %d", code)
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }
