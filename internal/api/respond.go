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

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
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
