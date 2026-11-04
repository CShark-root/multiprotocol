package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
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
	"panel/internal/panel/config"
	"panel/internal/panel/service"
	"panel/internal/panel/stealth"
	"panel/internal/panel/store"
)

const (
	base    = "TestBasePath123"
	subPath = "TestSubPath456"
)

type client struct {
	t      *testing.T
	url    string // panel prefix
	server string // server root
	http   *http.Client
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
		Users:    service.NewUsers(st, nil),
		Inbounds: service.NewInbounds(st, nil, 1),
		Agent:    agent.New(),
		Web: fstest.MapFS{
			"index.html":      {Data: []byte("<html><head><title>x</title></head></html>")},
			"assets/app-1.js": {Data: []byte("console.log(1)")},
		},
		PublicHost: "203.0.113.5",
		SubBaseURL: "https://203.0.113.5:4000/" + subPath + "/",
	})
	sub := api.NewSubscription(api.SubscriptionDeps{SubPath: subPath, Store: st, PublicHost: "203.0.113.5", Decoy: config.DecoyNotFound})
	srv := httptest.NewServer(stealth.Gate(config.DecoyNotFound, map[string]http.Handler{base: h, subPath: sub}))
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &client{t: t, url: srv.URL + "/" + base, server: srv.URL, http: &http.Client{Jar: jar}}
}

// freePort returns a TCP port that is free right now.
func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestInboundsAndSubscription(t *testing.T) {
	c := newClient(t)
	c.expect(200, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "a-long-enough-password"})

	port := freePort(t)
	c.expect(400, "POST", "/api/inbounds", map[string]any{"remark": "x", "transport": "quic", "port": port, "target": "www.microsoft.com"})
	c.expect(400, "POST", "/api/inbounds", map[string]any{"remark": "x", "transport": "xhttp", "port": port, "target": "not a host"})
	c.expect(400, "POST", "/api/inbounds", map[string]any{"remark": "x", "transport": "xhttp", "port": 1, "target": "www.microsoft.com"})

	ib := c.expect(201, "POST", "/api/inbounds", map[string]any{"remark": "Основной", "transport": "xhttp", "port": port, "target": "www.microsoft.com"})
	if ib["publicKey"] == "" || ib["shortId"] == "" || ib["xhttpPath"] == "" || ib["serverName"] != "www.microsoft.com" || ib["target"] != "www.microsoft.com:443" {
		t.Fatalf("defaults not filled: %v", ib)
	}
	if _, leaked := ib["privateKey"]; leaked {
		t.Fatal("private key must never be returned")
	}
	c.expect(400, "POST", "/api/inbounds", map[string]any{"remark": "dup", "transport": "tcp", "port": port, "target": "www.microsoft.com"})

	ibPath := "/api/inbounds/" + itoa(int(ib["id"].(float64)))
	upd := c.expect(200, "PUT", ibPath, map[string]any{"remark": "Main", "transport": "xhttp", "port": port, "target": "www.apple.com", "xhttpPath": ib["xhttpPath"]})
	if upd["publicKey"] != ib["publicKey"] || upd["serverName"] != "www.apple.com" {
		t.Fatalf("keys must survive updates: %v", upd)
	}

	// Hysteria2 listens on UDP, so it may share the port number with Reality.
	hy := c.expect(201, "POST", "/api/inbounds", map[string]any{"remark": "Hy2", "protocol": "hysteria2", "port": port, "obfs": true})
	if hy["network"] != "udp" || hy["serverName"] != "www.bing.com" || hy["obfs"] != true || hy["certPin"] == "" {
		t.Fatalf("hysteria defaults not filled: %v", hy)
	}
	for _, secret := range []string{"obfsPassword", "keyPem", "certPem"} {
		if _, leaked := hy[secret]; leaked {
			t.Fatalf("%s must never be returned", secret)
		}
	}
	c.expect(400, "POST", "/api/inbounds", map[string]any{"remark": "Hy2 dup", "protocol": "hysteria2", "port": port})
	c.expect(400, "POST", "/api/inbounds", map[string]any{"remark": "bad", "protocol": "hysteria2", "port": freePort(t), "masquerade": "http://plain.example.com"})

	u := c.expect(201, "POST", "/api/users", map[string]any{"name": "phone"})
	links := c.expect(200, "GET", "/api/users/"+itoa(int(u["id"].(float64)))+"/links", nil)
	list := links["links"].([]any)
	if len(list) != 2 || !strings.HasPrefix(list[0].(map[string]any)["url"].(string), "vless://") ||
		!strings.HasPrefix(list[1].(map[string]any)["url"].(string), "hysteria2://") {
		t.Fatalf("unexpected links: %v", links)
	}
	subURL := links["subscriptionUrl"].(string)
	if !strings.HasPrefix(subURL, "https://203.0.113.5:4000/"+subPath+"/") {
		t.Fatalf("bad subscription url %q", subURL)
	}
	token := subURL[strings.LastIndex(subURL, "/")+1:]

	resp, err := http.Get(c.server + "/" + subPath + "/" + token)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	decoded, err := base64.StdEncoding.DecodeString(string(body))
	if resp.StatusCode != 200 || err != nil || !strings.Contains(string(decoded), "type=xhttp") || !strings.Contains(string(decoded), "hysteria2://") {
		t.Fatalf("subscription: %d %q (%v)", resp.StatusCode, decoded, err)
	}
	if resp.Header.Get("Subscription-Userinfo") == "" {
		t.Fatal("missing subscription-userinfo header")
	}
	for _, bad := range []string{"/" + subPath + "/AAAAAAAAAAAAAAAAAAAAAAAAAA", "/" + subPath + "/x", "/" + subPath + "/"} {
		resp, err := http.Get(c.server + bad)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("%s: unknown tokens must look like any other miss, got %d", bad, resp.StatusCode)
		}
	}

	c.expect(204, "DELETE", ibPath, nil)
	if l := c.expect(200, "GET", "/api/users/"+itoa(int(u["id"].(float64)))+"/links", nil); len(l["links"].([]any)) != 1 {
		t.Fatalf("the VLESS link must disappear with its inbound: %v", l)
	}
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

func TestTestKit(t *testing.T) {
	c := newClient(t)
	c.expect(200, "POST", "/api/auth/setup", map[string]string{"username": "admin", "password": "a-long-enough-password"})

	kit := c.expect(201, "POST", "/api/inbounds/test-kit", map[string]any{})
	if kit["inbounds"].(float64) != 11 {
		t.Fatalf("test kit: %v", kit)
	}
	c.expect(409, "POST", "/api/inbounds/test-kit", map[string]any{})

	code, body := c.json("GET", "/api/inbounds", nil)
	var list []map[string]any
	json.Unmarshal(body, &list)
	if code != 200 || len(list) != 11 {
		t.Fatalf("inbounds after test kit: %d %d", code, len(list))
	}
	ports := map[string]bool{}
	for _, ib := range list {
		key := ib["network"].(string) + "/" + itoa(int(ib["port"].(float64)))
		if ports[key] {
			t.Fatalf("port collision %s", key)
		}
		ports[key] = true
	}

	code, body = c.json("GET", "/api/users", nil)
	var users []map[string]any
	json.Unmarshal(body, &users)
	if code != 200 || len(users) != 1 || users[0]["name"] != "test-kit" {
		t.Fatalf("test-kit user: %s", body)
	}
	links := c.expect(200, "GET", "/api/users/"+itoa(int(users[0]["id"].(float64)))+"/links", nil)
	withURL := 0
	for _, l := range links["links"].([]any) {
		if l.(map[string]any)["url"] != "" {
			withURL++
		}
	}
	if withURL != 10 { // all but ShadowTLS
		t.Fatalf("links with a share URL: %d", withURL)
	}

	if !strings.HasSuffix(links["singboxUrl"].(string), "?format=singbox") {
		t.Fatalf("singboxUrl: %v", links["singboxUrl"])
	}
	// SubBaseURL points at the public address; fetch through the test server.
	token := links["subscriptionUrl"].(string)
	token = token[strings.LastIndex(token, "/")+1:]
	resp, err := http.Get(c.server + "/" + subPath + "/" + token + "?format=singbox")
	if err != nil {
		t.Fatal(err)
	}
	var sb map[string]any
	err = json.NewDecoder(resp.Body).Decode(&sb)
	resp.Body.Close()
	if err != nil || resp.Header.Get("Content-Type") != "application/json; charset=utf-8" || sb["outbounds"] == nil {
		t.Fatalf("sing-box subscription: %v %v", err, sb)
	}

	del := c.expect(200, "DELETE", "/api/inbounds/test-kit", nil)
	if del["inbounds"].(float64) != 11 {
		t.Fatalf("delete test kit: %v", del)
	}
	if code, body := c.json("GET", "/api/users", nil); code != 200 || string(body) != "[]\n" {
		t.Fatalf("test-kit user must be removed: %s", body)
	}
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
