package api

import (
	"net/http"

	"panel/internal/agent"
	"panel/internal/shared/version"
)

type systemDTO struct {
	Version    string            `json:"version"`
	LocalAgent *agent.NodeHealth `json:"localAgent"`
}

func (s *server) system(w http.ResponseWriter, r *http.Request) {
	dto := systemDTO{Version: version.Version}
	if s.Agent != nil {
		h := s.Agent.Health(r.Context())
		dto.LocalAgent = &h
	}
	writeJSON(w, http.StatusOK, dto)
}
