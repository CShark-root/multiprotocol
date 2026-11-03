package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"panel/internal/panel/service"
	"panel/internal/panel/store"
)

type userDTO struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Note         string     `json:"note"`
	Enabled      bool       `json:"enabled"`
	Status       string     `json:"status"`
	TrafficLimit int64      `json:"trafficLimit"`
	TrafficUsed  int64      `json:"trafficUsed"`
	ResetPeriod  string     `json:"resetPeriod"`
	ExpireAt     *time.Time `json:"expireAt"`
	SubToken     string     `json:"subToken"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type userInputDTO struct {
	Name         string     `json:"name"`
	Note         string     `json:"note"`
	Enabled      *bool      `json:"enabled"` // omitted = true
	TrafficLimit int64      `json:"trafficLimit"`
	ResetPeriod  string     `json:"resetPeriod"`
	ExpireAt     *time.Time `json:"expireAt"`
}

func (s *server) toUserDTO(u store.User) userDTO {
	return userDTO{
		ID: u.ID, Name: u.Name, Note: u.Note, Enabled: u.Enabled,
		Status:       string(service.StatusOf(u, s.Users.Now())),
		TrafficLimit: u.TrafficLimit, TrafficUsed: u.TrafficUsed, ResetPeriod: u.ResetPeriod,
		ExpireAt: u.ExpireAt, SubToken: u.SubToken, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

func (in userInputDTO) toInput() service.UserInput {
	enabled := in.Enabled == nil || *in.Enabled
	return service.UserInput{
		Name: in.Name, Note: in.Note, Enabled: enabled,
		TrafficLimit: in.TrafficLimit, ResetPeriod: in.ResetPeriod, ExpireAt: in.ExpireAt,
	}
}

func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Users.List(r.Context())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	out := make([]userDTO, len(users))
	for i, u := range users {
		out[i] = s.toUserDTO(u)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) getUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u, err := s.Users.Get(r.Context(), id)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toUserDTO(u))
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	var in userInputDTO
	if !decodeJSON(w, r, &in) {
		return
	}
	u, err := s.Users.Create(r.Context(), adminFrom(r.Context()).ID, in.toInput())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.toUserDTO(u))
}

func (s *server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in userInputDTO
	if !decodeJSON(w, r, &in) {
		return
	}
	u, err := s.Users.Update(r.Context(), adminFrom(r.Context()).ID, id, in.toInput())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toUserDTO(u))
}

func (s *server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Users.Delete(r.Context(), adminFrom(r.Context()).ID, id); err != nil {
		s.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad_id", "invalid id")
		return 0, false
	}
	return id, true
}
