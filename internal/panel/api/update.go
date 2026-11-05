package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"panel/internal/panel/store"
	"panel/internal/shared/version"
)

// The updater: the panel (no root rights) writes update-request into its
// data directory; panel-update.path notices it and panel-update.service runs
// the checksum-verified installer of the latest release as root.
const updaterScript = "/usr/local/lib/panel/panel-update.sh"

type releaseInfo struct {
	Tag       string    `json:"tag"`
	URL       string    `json:"url"`
	Published time.Time `json:"published"`
}

var latestCache struct {
	sync.Mutex
	at   time.Time
	info *releaseInfo
	err  string
}

// latestRelease asks GitHub at most once an hour.
func latestRelease(ctx context.Context, force bool) (*releaseInfo, string) {
	c := &latestCache
	c.Lock()
	defer c.Unlock()
	if !force && time.Since(c.at) < time.Hour {
		return c.info, c.err
	}
	c.at = time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+version.Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		c.info, c.err = nil, err.Error()
		return c.info, c.err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.info, c.err = nil, "GitHub: HTTP "+resp.Status
		if resp.StatusCode == http.StatusNotFound {
			c.err = "no releases yet"
		}
		return c.info, c.err
	}
	var r struct {
		Tag       string    `json:"tag_name"`
		URL       string    `json:"html_url"`
		Published time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		c.info, c.err = nil, err.Error()
		return c.info, c.err
	}
	c.info, c.err = &releaseInfo{Tag: r.Tag, URL: r.URL, Published: r.Published}, ""
	return c.info, c.err
}

func (s *server) updateRequestPath() string { return filepath.Join(s.Panel.DataDir, "update-request") }

func (s *server) updateStatus(w http.ResponseWriter, r *http.Request) {
	latest, checkErr := latestRelease(r.Context(), r.URL.Query().Get("check") == "1")
	_, err := os.Stat(updaterScript)
	out := map[string]any{
		"current":   version.Version,
		"repo":      version.Repo,
		"latest":    latest,
		"error":     checkErr,
		"supported": runtime.GOOS == "linux" && err == nil,
	}
	_, pending := os.Stat(s.updateRequestPath())
	out["pending"] = pending == nil
	if b, err := os.ReadFile(filepath.Join(s.Panel.DataDir, "update.log")); err == nil {
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(lines) > 200 {
			lines = lines[len(lines)-200:]
		}
		out["log"] = lines
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) startUpdate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Cores bool `json:"cores"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if _, err := os.Stat(updaterScript); runtime.GOOS != "linux" || err != nil {
		writeError(w, http.StatusNotImplemented, "unsupported", "the updater is not installed here; run install.sh once")
		return
	}
	body := "requested-by=panel\n"
	if in.Cores {
		body += "cores=1\n"
	}
	path := s.updateRequestPath()
	if err := os.WriteFile(path+".new", []byte(body), 0o600); err != nil {
		s.handleError(w, r, err)
		return
	}
	if err := os.Rename(path+".new", path); err != nil {
		s.handleError(w, r, errors.Join(err, os.Remove(path+".new")))
		return
	}
	s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: adminFrom(r.Context()).ID, Action: "panel.update", TargetType: "system",
		Diff: map[string]any{"from": version.Version, "cores": in.Cores}}, time.Now())
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}
