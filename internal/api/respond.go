package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/TheOriUHD/aperture/internal/store"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeErrorCode(w, status, codeForStatus(status), msg)
}

// writeErrorCode writes the v1 error envelope: a stable machine-readable `code`
// for programmatic handling alongside the human `error` prose (which the web app
// still reads, so adding `code` is non-breaking). Third-party clients match on
// `code`, never the English string. Handlers wanting a code more specific than
// the status default (e.g. forbidden.camera vs forbidden.scope) call this
// directly; everything else goes through writeError.
func writeErrorCode(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// codeForStatus is the default machine code for an HTTP status.
func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "validation"
	case http.StatusUnauthorized:
		return "unauthenticated"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusPreconditionFailed:
		return "precondition_failed"
	case http.StatusPreconditionRequired:
		return "precondition_required"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large"
	case http.StatusUnsupportedMediaType:
		return "unsupported"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "unavailable"
	case http.StatusInternalServerError:
		return "internal"
	default:
		return "error"
	}
}

// writeStoreError maps store errors to HTTP responses.
func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "internal error")
}

// decodeJSON reads a JSON body into v, rejecting unknown fields.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// msFromSec converts a unix-seconds column (e.g. created_at/updated_at, stored in
// seconds) to unix milliseconds for the wire. v1 normalizes every timestamp to
// unix ms and names the field with an "Ms" suffix so the unit is unambiguous
// (DD-WIRE-TIME); storage is left as-is. Fields already stored in ms (segments/
// events *_ts) are emitted directly.
func msFromSec(sec int64) int64 { return sec * 1000 }
