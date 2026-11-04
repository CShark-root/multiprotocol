package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"panel/internal/panel/auth"
	"panel/internal/panel/store"
)

func (s *server) currentToken(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// authError maps account errors to responses with stable codes the UI
// understands.
func (s *server) authError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrWrongPassword):
		writeJSON(w, http.StatusForbidden, map[string]apiError{"error": {Code: "wrong_password", Message: err.Error(), Field: "password"}})
	case errors.Is(err, auth.ErrInvalidTOTP):
		writeJSON(w, http.StatusForbidden, map[string]apiError{"error": {Code: "invalid_totp", Message: err.Error(), Field: "code"}})
	default:
		s.handleError(w, r, err)
	}
}

func (s *server) account(w http.ResponseWriter, r *http.Request) {
	a := adminFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"username": a.Username, "totpEnabled": a.TOTPSecret != ""})
}

func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	a := adminFrom(r.Context())
	if err := s.Auth.ChangePassword(r.Context(), a, in.Current, in.Next, s.currentToken(r)); err != nil {
		s.authError(w, r, err)
		return
	}
	s.audit(r, "account.password", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) totpSetup(w http.ResponseWriter, r *http.Request) {
	secret := auth.NewTOTPSecret()
	writeJSON(w, http.StatusOK, map[string]string{
		"secret": secret,
		"uri":    auth.TOTPURI(secret, adminFrom(r.Context()).Username, "MultiProtocol"),
	})
}

func (s *server) totpEnable(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Secret string `json:"secret"`
		Code   string `json:"code"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.Auth.EnableTOTP(r.Context(), adminFrom(r.Context()), in.Secret, in.Code); err != nil {
		s.authError(w, r, err)
		return
	}
	s.audit(r, "account.totp_on", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) totpDisable(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := s.Auth.DisableTOTP(r.Context(), adminFrom(r.Context()), in.Password, in.Code); err != nil {
		s.authError(w, r, err)
		return
	}
	s.audit(r, "account.totp_off", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listSessions(w http.ResponseWriter, r *http.Request) {
	list, err := s.Auth.Sessions(r.Context(), adminFrom(r.Context()).ID, s.currentToken(r))
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *server) revokeSession(w http.ResponseWriter, r *http.Request) {
	if err := s.Auth.RevokeSession(r.Context(), adminFrom(r.Context()).ID, chi.URLParam(r, "id")); err != nil {
		s.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	n, err := s.Auth.RevokeOtherSessions(r.Context(), adminFrom(r.Context()).ID, s.currentToken(r))
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	s.audit(r, "account.sessions_revoked", map[string]int64{"sessions": n})
	writeJSON(w, http.StatusOK, map[string]int64{"revoked": n})
}

// audit records an account or settings change made by the current admin.
func (s *server) audit(r *http.Request, action string, diff any) {
	if s.Store == nil {
		return
	}
	e := store.AuditEntry{AdminID: adminFrom(r.Context()).ID, Action: action, TargetType: "panel", TargetID: "0", Diff: diff}
	if err := s.Store.AddAudit(r.Context(), e, s.Users.Now()); err != nil {
		s.Logger.Warn("audit log write failed", "action", action, "err", err)
	}
}
