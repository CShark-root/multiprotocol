package api

import (
	"errors"
	"net/http"
	"strconv"

	"panel/internal/amnezia"
)

// amneziaStatus reports the Amnezia containers through the read-only
// helper. "helper": false means the helper service is not running (dev
// machines, or the installer has not been run since it was added).
func (s *server) amneziaStatus(w http.ResponseWriter, r *http.Request) {
	if s.Amnezia == nil {
		writeJSON(w, http.StatusOK, map[string]any{"helper": false})
		return
	}
	st, err := s.Amnezia.Status(r.Context())
	if errors.Is(err, amnezia.ErrNoHelper) {
		writeJSON(w, http.StatusOK, map[string]any{"helper": false})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "helper", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"helper": true, "status": st})
}

func (s *server) amneziaLogs(w http.ResponseWriter, r *http.Request) {
	if s.Amnezia == nil {
		writeError(w, http.StatusConflict, "no_helper", "amnezia helper is not running")
		return
	}
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if tail <= 0 {
		tail = 200
	}
	lines, err := s.Amnezia.Logs(r.Context(), r.URL.Query().Get("name"), tail)
	if err != nil {
		writeError(w, http.StatusBadGateway, "helper", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, lines)
}
