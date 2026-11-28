package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"findus/backend/internal/domain"
)

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writeJSONError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// writeError passes validation, not-found and conflict messages on to the client. Everything else
// may carry paths or SQL, so it only goes to the log.
func (s *Server) writeError(w http.ResponseWriter, status int, err error) {
	s.writeJSONError(w, status, s.clientMessage(err))
}

func (s *Server) clientMessage(err error) string {
	switch {
	case errors.Is(err, domain.ErrValidation), errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrConflict):
		return err.Error()
	case strings.Contains(err.Error(), "UNIQUE constraint failed"):
		return "an entry with this value already exists"
	}
	s.Log.Error("request failed", "err", err)
	return "request failed"
}
