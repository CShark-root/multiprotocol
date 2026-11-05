package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"panel/internal/amnezia"
	"panel/internal/panel/service"
	"panel/internal/panel/store"
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
	links, err := s.Store.ListAmneziaLinks(r.Context())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"helper": true, "status": st, "links": links})
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

// amneziaChangeError reports a refused or failed client change.
func (s *server) amneziaChangeError(w http.ResponseWriter, err error) {
	var ce *amnezia.ChangeError
	switch {
	case errors.Is(err, amnezia.ErrNoHelper):
		writeError(w, http.StatusConflict, "no_helper", "помощник Amnezia не запущен")
	case errors.As(err, &ce):
		writeError(w, http.StatusConflict, "refused", ce.Msg)
	default:
		writeError(w, http.StatusBadGateway, "helper", err.Error())
	}
}

// addAmneziaClient creates an AmneziaWG client the way the Amnezia app
// does. With userId the config is also saved on that panel user, so it
// shows under Connect a device.
func (s *server) addAmneziaClient(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Container string `json:"container"`
		Name      string `json:"name"`
		UserID    int64  `json:"userId"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if s.Amnezia == nil {
		writeError(w, http.StatusConflict, "no_helper", "помощник Amnezia не запущен")
		return
	}
	var user *store.User
	if in.UserID > 0 {
		u, err := s.Users.Get(r.Context(), in.UserID)
		if err != nil {
			s.handleError(w, r, err)
			return
		}
		user = &u
	}
	nc, err := s.Amnezia.AddClient(r.Context(), amnezia.AddParams{Container: in.Container, Name: in.Name, Host: s.PublicHost})
	if err != nil {
		s.amneziaChangeError(w, err)
		return
	}
	admin := adminFrom(r.Context()).ID
	s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: admin, Action: "amnezia.client_add", TargetType: "amnezia", TargetID: in.Container,
		Diff: map[string]any{"name": in.Name, "ip": nc.IP, "key": nc.Key}}, time.Now())
	saved := false
	if user != nil {
		// Привязка: срок и лимит трафика пользователя действуют и на этого клиента.
		if err := s.Store.LinkAmnezia(r.Context(), in.Container, nc.Key, user.ID); err != nil {
			s.Logger.Warn("link amnezia client", "err", err)
		}
		s.trigger()
		in2 := service.UserInput{
			Name: user.Name, Note: user.Note, Enabled: user.Enabled, TrafficLimit: user.TrafficLimit, ResetPeriod: user.ResetPeriod,
			ExpireAt: user.ExpireAt, AmneziaKey: nc.VPNKey, SubHidden: user.SubHidden,
		}
		if _, err := s.Users.Update(r.Context(), admin, user.ID, in2); err == nil {
			saved = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": nc.Key, "ip": nc.IP, "config": nc.Config, "vpnKey": nc.VPNKey, "version": nc.Version, "savedToUser": saved})
}

func (s *server) changeAmneziaClient(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in amnezia.KeyParams
		if !decodeJSON(w, r, &in) {
			return
		}
		if s.Amnezia == nil {
			writeError(w, http.StatusConflict, "no_helper", "помощник Amnezia не запущен")
			return
		}
		var err error
		if action == "remove" {
			err = s.Amnezia.RemoveClient(r.Context(), in)
			if err == nil {
				s.Store.LinkAmnezia(r.Context(), in.Container, in.Key, 0)
			}
		} else {
			err = s.Amnezia.RenameClient(r.Context(), in)
		}
		if err != nil {
			s.amneziaChangeError(w, err)
			return
		}
		s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: adminFrom(r.Context()).ID, Action: "amnezia.client_" + action,
			TargetType: "amnezia", TargetID: in.Container, Diff: map[string]any{"key": in.Key, "name": in.Name}}, time.Now())
		w.WriteHeader(http.StatusNoContent)
	}
}

// linkAmneziaClient привязывает клиента AmneziaWG к пользователю панели
// (userId 0 — отвязывает). Файлы Amnezia не меняются.
func (s *server) linkAmneziaClient(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Container string `json:"container"`
		Key       string `json:"key"`
		UserID    int64  `json:"userId"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !amnezia.Manageable(in.Container) {
		writeError(w, http.StatusBadRequest, "container", "этим контейнером панель не управляет")
		return
	}
	if len(in.Key) != 44 {
		writeError(w, http.StatusBadRequest, "key", "неверный ключ клиента")
		return
	}
	if in.UserID > 0 {
		if _, err := s.Users.Get(r.Context(), in.UserID); err != nil {
			s.handleError(w, r, err)
			return
		}
	}
	if err := s.Store.LinkAmnezia(r.Context(), in.Container, in.Key, in.UserID); err != nil {
		s.handleError(w, r, err)
		return
	}
	s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: adminFrom(r.Context()).ID, Action: "amnezia.client_link",
		TargetType: "amnezia", TargetID: in.Container, Diff: map[string]any{"key": in.Key, "userId": in.UserID}}, time.Now())
	s.trigger()
	w.WriteHeader(http.StatusNoContent)
}

// trigger просит синхронизацию применить изменения сразу.
func (s *server) trigger() {
	if s.Syncer != nil {
		s.Syncer.Trigger()
	}
}
