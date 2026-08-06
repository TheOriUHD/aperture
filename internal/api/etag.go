package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/TheOriUHD/aperture/internal/auth"
)

// ETag / If-Match optimistic concurrency (finishing 1.9 — only the idempotency
// half shipped earlier). ETag is a strong validator computed from the resource's
// current representation. The plan specified hash(updated_at+id), but most
// mutable resources (rules, channels, automations, webhooks, users) carry no
// updated_at, so a content hash is used uniformly — it subsumes updated_at+id
// (any change, including to updated_at where present, changes the hash) and
// works for the singleton config blobs (settings, site, detection) too.
//
// If-Match policy by credential class:
//   - class=token: REQUIRED on every config write. Missing → 428
//     Precondition Required; present-but-stale → 412 Precondition Failed. A
//     scoped extension must prove it saw the current state before overwriting.
//   - class=session / class=cli: OPTIONAL. Enforced when present (stale → 412),
//     ignored when absent (last-writer-wins) so the current web app — which
//     sends none until 5.2 — keeps working.

// etagOf returns a strong ETag for a resource's current representation.
func etagOf(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// setETag stamps the ETag header for v and returns it.
func setETag(w http.ResponseWriter, v any) string {
	e := etagOf(v)
	w.Header().Set("ETag", e)
	return e
}

// withETag renders a struct as a JSON object with an added "etag" field, so a
// LIST response exposes each item's ETag. The ETag is computed from the struct
// (not the rendered map), so it equals the precondition's etagOf(item) — a token
// can list, then send that ETag as If-Match and get a real 412 on staleness
// rather than only If-Match: * (existence). Used for the raw-struct collections
// (rules/automations/webhooks); the view-backed ones add "etag" to their view.
func withETag(v any) map[string]any {
	e := etagOf(v)
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{"etag": e}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || m == nil {
		return map[string]any{"etag": e}
	}
	m["etag"] = e
	return m
}

// precondition enforces the If-Match policy for a mutating request against the
// resource's current ETag. It returns false — having already written the 428/412
// response — when the request must be rejected; true to proceed with the write.
func (s *Server) precondition(w http.ResponseWriter, r *http.Request, current string) bool {
	im := strings.TrimSpace(r.Header.Get("If-Match"))
	if im == "" {
		if c := auth.FromContext(r.Context()); c != nil && c.Class == auth.ClassToken {
			writeErrorCode(w, http.StatusPreconditionRequired, "precondition_required",
				"If-Match header is required for token writes")
			return false
		}
		return true // session/cli may omit (last-writer-wins until 5.2)
	}
	if !ifMatchSatisfied(im, current) {
		writeErrorCode(w, http.StatusPreconditionFailed, "precondition_failed",
			"If-Match does not match the current resource (it changed since you read it)")
		return false
	}
	return true
}

// ifMatchSatisfied supports "*" (matches any existing resource) and a
// comma-separated list of candidate ETags.
func ifMatchSatisfied(header, current string) bool {
	if header == "*" {
		return true
	}
	for _, tok := range strings.Split(header, ",") {
		if strings.TrimSpace(tok) == current {
			return true
		}
	}
	return false
}
