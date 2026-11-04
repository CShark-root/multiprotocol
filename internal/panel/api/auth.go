package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"panel/internal/panel/auth"
	"panel/internal/panel/service"
	"panel/internal/panel/store"
)

const sessionCookie = "sid"

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type adminDTO struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type authStateDTO struct {
	SetupRequired bool      `json:"setupRequired"`
	Authenticated bool      `json:"authenticated"`
	Admin         *adminDTO `json:"admin,omitempty"`
}

func toAdminDTO(a store.Admin) *adminDTO { return &adminDTO{ID: a.ID, Username: a.Username} }

func (s *server) authState(w http.ResponseWriter, r *http.Request) {
	setup, err := s.Auth.SetupRequired(r.Context())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	st := authStateDTO{SetupRequired: setup}
	if a, ok := s.currentAdmin(r); ok {
		st.Authenticated, st.Admin = true, toAdminDTO(a)
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *server) setup(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decodeJSON(w, r, &in) {
		return
	}
	admin, err := s.Auth.Setup(r.Context(), in.Username, in.Password)
	if errors.Is(err, auth.ErrAlreadySetup) {
		writeError(w, http.StatusConflict, "already_setup", err.Error())
		return
	}
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	token, err := s.Auth.CreateSession(r.Context(), admin.ID, clientIP(r), r.UserAgent())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]*adminDTO{"admin": toAdminDTO(admin)})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decodeJSON(w, r, &in) {
		return
	}
	token, admin, err := s.Auth.Login(r.Context(), in.Username, in.Password, clientIP(r), r.UserAgent())
	if errors.Is(err, auth.ErrInvalidCredentials) {
		s.Logger.Warn("failed login", "ip", clientIP(r))
		s.Events.Add(r.Context(), service.EventLoginFailed, "warning", 0, "",
			map[string]any{"ip": clientIP(r), "username": truncate(in.Username, 64)})
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	s.Logger.Info("admin logged in", "admin", admin.Username, "ip", clientIP(r))
	s.Events.Add(r.Context(), service.EventLogin, "info", 0, "", map[string]any{"ip": clientIP(r), "admin": admin.Username})
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]*adminDTO{"admin": toAdminDTO(admin)})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.Auth.Logout(r.Context(), c.Value); err != nil {
			s.handleError(w, r, err)
			return
		}
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     s.prefix + "/", // never sent outside the secret path
		MaxAge:   int(auth.SessionTTL / time.Second),
		HttpOnly: true,
		Secure:   s.TLS,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: s.prefix + "/", MaxAge: -1,
		HttpOnly: true, Secure: s.TLS, SameSite: http.SameSiteStrictMode,
	})
}

type ctxKey int

const adminKey ctxKey = 0

func (s *server) currentAdmin(r *http.Request) (store.Admin, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return store.Admin{}, false
	}
	a, err := s.Auth.Authenticate(r.Context(), c.Value)
	if err != nil && !errors.Is(err, auth.ErrUnauthenticated) {
		s.Logger.Error("session lookup failed", "err", err)
	}
	return a, err == nil
}

func (s *server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, ok := s.currentAdmin(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "login required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminKey, a)))
	})
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func adminFrom(ctx context.Context) store.Admin {
	a, _ := ctx.Value(adminKey).(store.Admin)
	return a
}

func (s *server) limitLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.loginLimiter.allow(clientIP(r), time.Now()) {
			s.Logger.Warn("login rate limit hit", "ip", clientIP(r))
			s.Events.Add(r.Context(), service.EventLoginThrottle, "danger", 0, "", map[string]any{"ip": clientIP(r)})
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later")
			return
		}
		next.ServeHTTP(w, r)
	})
}
