package api

import (
	"net/http"
	"time"

	"panel/internal/panel/service"
)

func (s *server) trafficSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, service.TrafficSettingsOf(r.Context(), s.Store))
}

func (s *server) saveTrafficSettings(w http.ResponseWriter, r *http.Request) {
	var in service.TrafficSettings
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.Store.SetSetting(r.Context(), service.TrafficSettingsKey, in); err != nil {
		s.handleError(w, r, err)
		return
	}
	if s.Syncer != nil {
		s.Syncer.SetDetailedTraffic(in.Detailed)
	}
	if err := s.Store.PruneTrafficMinutes(r.Context(), time.Now().Add(-7*24*time.Hour).Unix()); err != nil {
		s.handleError(w, r, err)
		return
	}
	s.audit(r, "traffic.settings", map[string]any{"detailed": in.Detailed})
	writeJSON(w, http.StatusOK, in)
}
