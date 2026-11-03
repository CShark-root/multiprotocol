package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"panel/internal/panel/store"
	"panel/internal/panel/validate"
)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: message}})
}

// handleError maps service and store errors to responses. Unknown errors are
// logged and reported as a bare 500 so internals never reach the client.
func (s *server) handleError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *validate.Error
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]apiError{
			"error": {Code: "validation", Message: ve.Message, Field: ve.Field},
		})
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "already exists")
	default:
		s.Logger.Error("request failed", "method", r.Method, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

const maxBodyBytes = 1 << 20

// decodeJSON reads exactly one JSON object, rejecting unknown fields and
// oversized bodies. It writes the error response itself and returns false.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "invalid JSON body: "+err.Error())
		return false
	}
	if dec.More() {
		writeError(w, http.StatusBadRequest, "bad_json", "body must contain a single JSON object")
		return false
	}
	return true
}
