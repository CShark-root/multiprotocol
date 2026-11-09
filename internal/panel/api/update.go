package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/panel/service"
	"panel/internal/panel/store"
	"panel/internal/shared/version"
)

func (s *server) updateRequestPath() string { return service.UpdateRequestPath(s.Panel.DataDir) }

func (s *server) updateStatus(w http.ResponseWriter, r *http.Request) {
	latest, checkErr := service.LatestRelease(r.Context(), r.URL.Query().Get("check") == "1")
	out := map[string]any{
		"current":   version.Version,
		"repo":      version.Repo,
		"latest":    latest,
		"error":     checkErr,
		"supported": service.UpdaterInstalled(),
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
	if !service.UpdaterInstalled() {
		writeError(w, http.StatusNotImplemented, "unsupported", "the updater is not installed here; run install.sh once")
		return
	}
	if err := service.RequestUpdate(s.Panel.DataDir, in.Cores); err != nil {
		s.handleError(w, r, err)
		return
	}
	s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: adminFrom(r.Context()).ID, Action: "panel.update", TargetType: "system",
		Diff: map[string]any{"from": version.Version, "cores": in.Cores}}, time.Now())
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

// coresAuto — настройки автообновления ядер.
func (s *server) coresAuto(w http.ResponseWriter, r *http.Request) {
	if s.Cores == nil {
		writeJSON(w, http.StatusOK, map[string]any{"supported": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"supported": s.Cores.Supported(), "settings": s.Cores.Get(r.Context()), "clock": serverClock()})
}

func (s *server) saveCoresAuto(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
		Hour    int  `json:"hour"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if s.Cores == nil {
		writeError(w, http.StatusNotImplemented, "unsupported", "no local cores")
		return
	}
	if in.Hour < 0 || in.Hour > 23 {
		writeError(w, http.StatusBadRequest, "hour", "0-23")
		return
	}
	out, err := s.Cores.Save(r.Context(), in.Enabled, in.Hour)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: adminFrom(r.Context()).ID, Action: "cores.auto", TargetType: "system",
		Diff: map[string]any{"enabled": in.Enabled, "hour": in.Hour}}, time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"supported": s.Cores.Supported(), "settings": out, "clock": serverClock()})
}

// serverClock — часы сервера: время проверки ядер задаётся по ним, а у админа
// в браузере может быть другой часовой пояс.
func serverClock() map[string]any {
	now := time.Now()
	zone, offset := now.Zone()
	return map[string]any{"time": now.Format("15:04"), "zone": zone, "offset": offset / 60}
}

// coreVersions — версии ядер для выбора, как в 3x-ui.
func (s *server) coreVersions(w http.ResponseWriter, r *http.Request) {
	if s.Cores == nil {
		writeJSON(w, http.StatusOK, map[string]any{"supported": false, "cores": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"supported": s.Cores.Supported(), "cores": s.Cores.Versions(r.Context())})
}

func (s *server) installCoreVersion(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Core    string `json:"core"`
		Version string `json:"version"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if s.Cores == nil {
		writeError(w, http.StatusNotImplemented, "unsupported", "no local cores")
		return
	}
	if err := s.Cores.Install(r.Context(), in.Core, in.Version); err != nil {
		if errors.Is(err, service.ErrUpdateBusy) {
			writeError(w, http.StatusConflict, "busy", "an update is already running")
			return
		}
		s.handleError(w, r, err)
		return
	}
	s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: adminFrom(r.Context()).ID, Action: "cores.install", TargetType: "system",
		Diff: map[string]any{"core": in.Core, "version": in.Version}}, time.Now())
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}
