package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jonasthim/styr/internal/domain"
)

// maxJSONBody bounds request bodies decoded by decodeJSON.
const maxJSONBody = 1 << 20 // 1 MiB

// errorEnvelope is the documented error response shape:
// {"error":{"code":"not_found","message":"..."}}.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON writes v as a JSON body with the given status. A nil v writes
// only the status line (used for 204 responses via w.WriteHeader instead).
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// writeErrorCode writes the documented error envelope with an explicit
// status and code, for errors that do not come from a domain sentinel
// (e.g. "token_invalid", "invalid_path").
func writeErrorCode(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}

// WriteError maps err to the documented error envelope: domain.ErrNotFound
// -> 404, domain.ErrForbidden -> 403, domain.ErrInvalid -> 422,
// domain.ErrConflict -> 409, anything else -> 500 with a generic message
// (the real error is not leaked to the client).
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeErrorCode(w, http.StatusNotFound, "not_found", userMessage(err, domain.ErrNotFound))
	case errors.Is(err, domain.ErrForbidden):
		writeErrorCode(w, http.StatusForbidden, "forbidden", userMessage(err, domain.ErrForbidden))
	case errors.Is(err, domain.ErrInvalid):
		writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", userMessage(err, domain.ErrInvalid))
	case errors.Is(err, domain.ErrConflict):
		writeErrorCode(w, http.StatusConflict, "conflict", userMessage(err, domain.ErrConflict))
	default:
		writeErrorCode(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

// decodeJSON decodes r's body into dst, bounded by maxJSONBody.
func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, maxJSONBody))
	return dec.Decode(dst)
}

// userMessage strips the sentinel's own text from a wrapped error so the API
// says "the workspace has no git remote configured" rather than
// "conflict: the workspace has no git remote configured". A bare sentinel keeps
// its text.
func userMessage(err, sentinel error) string {
	msg := err.Error()
	prefix := sentinel.Error() + ": "
	if strings.HasPrefix(msg, prefix) && len(msg) > len(prefix) {
		return msg[len(prefix):]
	}
	return msg
}

// derefString returns the string behind p, or "" when p is nil; JSON inputs
// carry nullable ids while the domain uses "" for "not set".
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
