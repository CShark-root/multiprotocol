package api

import (
	"net/http"
	"time"

	"panel/internal/agent"
	"panel/internal/panel/store"
	"panel/internal/shared/version"
)

type syncDTO struct {
	LastSync  *time.Time `json:"lastSync"`
	LastError string     `json:"lastError"`
}

type systemDTO struct {
	Version    string            `json:"version"`
	PublicHost string            `json:"publicHost"`
	LocalAgent *agent.NodeHealth `json:"localAgent"`
	Sync       *syncDTO          `json:"sync"`
}

func (s *server) system(w http.ResponseWriter, r *http.Request) {
	dto := systemDTO{Version: version.Version, PublicHost: s.PublicHost}
	if s.Agent != nil {
		h := s.Agent.Health(r.Context())
		dto.LocalAgent = &h
	}
	if s.Syncer != nil {
		last, errMsg := s.Syncer.Status()
		dto.Sync = &syncDTO{LastError: errMsg}
		if !last.IsZero() {
			dto.Sync.LastSync = &last
		}
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *server) restartCores(w http.ResponseWriter, r *http.Request) {
	if s.Agent == nil {
		writeError(w, http.StatusConflict, "no_agent", "no local agent")
		return
	}
	err := s.Agent.RestartCores()
	s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: adminFrom(r.Context()).ID, Action: "cores.restart", TargetType: "system"}, time.Now())
	if err != nil {
		writeError(w, http.StatusBadGateway, "restart_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// rebootServer restarts the whole machine after the admin re-enters the
// password. systemctl only queues the reboot, so a refusal (no polkit rule)
// comes back as an error instead of a false "rebooting".
func (s *server) rebootServer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if s.Reboot == nil {
		writeError(w, http.StatusNotImplemented, "unsupported", "reboot is not available here")
		return
	}
	admin := adminFrom(r.Context())
	if err := s.Auth.ConfirmPassword(admin, in.Password); err != nil {
		s.authError(w, r, err)
		return
	}
	s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: admin.ID, Action: "server.reboot", TargetType: "system"}, time.Now())
	s.Logger.Warn("server reboot requested", "admin", admin.Username)
	if err := s.Reboot(); err != nil {
		s.Logger.Error("reboot failed", "err", err)
		writeError(w, http.StatusBadGateway, "reboot_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"rebooting": true})
}
