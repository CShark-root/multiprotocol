package hysteria

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
)

func TestAuthEndpointAndUserRemoval(t *testing.T) {
	a := New(filepath.Join(t.TempDir(), "missing-hysteria"), t.TempDir(), slog.Default())
	state, _ := json.Marshal(State{Users: map[string]string{"pw-1": "u1", "pw-2": "u2"}})
	if err := a.Apply(context.Background(), state); err != nil {
		t.Fatal(err)
	}

	check := func(password string) (bool, string) {
		body, _ := json.Marshal(map[string]any{"addr": "203.0.113.9:5000", "auth": password, "tx": 0})
		resp, err := http.Post(a.authURL, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			OK bool   `json:"ok"`
			ID string `json:"id"`
		}
		json.NewDecoder(resp.Body).Decode(&out)
		return out.OK, out.ID
	}
	if ok, id := check("pw-1"); !ok || id != "u1" {
		t.Fatalf("valid password rejected: %v %q", ok, id)
	}
	if ok, _ := check("nope"); ok {
		t.Fatal("unknown password accepted")
	}

	if removed := a.setUsers(map[string]string{"pw-2": "u2"}); len(removed) != 1 || removed[0] != "u1" {
		t.Fatalf("removed = %v, want [u1]", removed)
	}
	if ok, _ := check("pw-1"); ok {
		t.Fatal("removed user still authenticates")
	}

	// The endpoint path is secret: the bare port must not answer auth.
	resp, err := http.Post(a.authURL[:len(a.authURL)-5], "application/json", bytes.NewReader([]byte(`{}`)))
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatal("auth answered on a wrong path")
		}
	}
}

func TestInstanceWithoutBinary(t *testing.T) {
	a := New(filepath.Join(t.TempDir(), "missing-hysteria"), t.TempDir(), slog.Default())
	state, _ := json.Marshal(State{Instances: []Instance{{Tag: "in-1", Server: map[string]any{"listen": ":443"}}}})
	if err := a.Apply(context.Background(), state); err == nil {
		t.Fatal("expected an error without the binary")
	}
	h := a.Health(context.Background())
	if h.Installed || h.Running || h.Message == "" {
		t.Fatalf("unexpected health: %+v", h)
	}
	if err := a.Apply(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestParseVersion(t *testing.T) {
	out := "Hysteria 2\nVersion:\tv2.6.5\nBuildDate:\t2025-01-01\n"
	if v := parseVersion(out); v != "2.6.5" {
		t.Fatalf("got %q", v)
	}
}
