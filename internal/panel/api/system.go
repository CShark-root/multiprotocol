package api

import (
	"net/http"
	"time"

	"panel/internal/agent"
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
