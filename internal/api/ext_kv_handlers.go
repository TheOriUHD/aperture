package api

import (
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/ext"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Per-extension private KV (the `storage: true` permission), reachable by BOTH
// transports: a backend extension's token, and a UI panel via the host bridge (a
// session). Namespaced by ext_id; a token may only touch its OWN namespace, so
// extension code can never read another extension's storage. Destroyed on
// uninstall. Writes pass a small global limiter so a chatty extension can't starve
// the single SQLite writer beyond a bounded rate.

const maxExtKVKeyLen = 256

// extKVWrites bounds ext_kv write throughput across ALL extensions, protecting the
// single shared SQLite writer independently of the per-principal HTTP limiter. A
// generous steady rate for config/state; bursts absorb normal save flurries.
var extKVWrites = newTokenBucket(25, 50) // 25 writes/sec, burst 50

// extKVGuard resolves the namespace and enforces access. The extension must exist
// and have declared `storage`. Then, by caller class:
//   - a TOKEN (a backend extension) may reach ONLY its own ext_id — never another
//     extension's — role-independent;
//   - a SESSION/CLI (a human) may reach any namespace but MUST be an admin.
//
// Raw KV is admin-only for humans because storage is the extension's private config
// with no per-key ACL, so any read exposes every key — including secrets like an API
// token. A UI panel reaches storage through the host bridge under the VIEWER'S own
// session, and a viewer can call window.aperture.storage from the sandboxed iframe's
// console, so a "viewer via the bridge" allowance would leak those secrets exactly as
// a direct curl would. Viewer-facing data must instead come from the extension's
// backend through the broker, where it can be scoped; raw storage stays admin-only.
// Returns (0,"") when allowed.
func (s *Server) extKVGuard(r *http.Request, id string) (int, string) {
	ex, err := s.st.GetExtension(id)
	if err != nil {
		return http.StatusNotFound, "not found"
	}
	if !manifestHasStorage(ex.ManifestJSON) {
		return http.StatusForbidden, "this extension did not request storage"
	}
	c := auth.FromContext(r.Context())
	if c == nil {
		return http.StatusUnauthorized, "authentication required"
	}
	if c.Class == auth.ClassToken {
		if c.ExtID != id {
			return http.StatusForbidden, "an extension may only access its own storage"
		}
		return 0, ""
	}
	if c.Role != store.RoleAdmin {
		return http.StatusForbidden, "extension storage is available to administrators only"
	}
	return 0, ""
}

func manifestHasStorage(manifestJSON string) bool {
	m, err := ext.Parse([]byte(manifestJSON))
	return err == nil && m.Permissions.Storage
}

func extKVKey(r *http.Request) (string, bool) {
	k := chi.URLParam(r, "key")
	if k == "" || len(k) > maxExtKVKeyLen || strings.ContainsAny(k, "\x00") {
		return "", false
	}
	return k, true
}

func (s *Server) handleExtKVList(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if code, msg := s.extKVGuard(r, id); code != 0 {
		writeError(w, code, msg)
		return
	}
	keys, err := s.st.ExtKVKeys(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

func (s *Server) handleExtKVGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if code, msg := s.extKVGuard(r, id); code != 0 {
		writeError(w, code, msg)
		return
	}
	key, ok := extKVKey(r)
	if !ok {
		writeErrorCode(w, http.StatusBadRequest, "validation", "invalid key")
		return
	}
	v, found, err := s.st.ExtKVGet(id, key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "no such key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": v})
}

func (s *Server) handleExtKVSet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if code, msg := s.extKVGuard(r, id); code != 0 {
		writeError(w, code, msg)
		return
	}
	key, ok := extKVKey(r)
	if !ok {
		writeErrorCode(w, http.StatusBadRequest, "validation", "invalid key")
		return
	}
	var req struct {
		Value string `json:"value"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	if !extKVWrites.allow() {
		writeError(w, http.StatusTooManyRequests, "storage is being written too quickly; slow down")
		return
	}
	if err := s.st.ExtKVSet(id, key, req.Value); err != nil {
		if err == store.ErrExtKVQuota {
			writeErrorCode(w, http.StatusInsufficientStorage, "quota_exceeded", "extension storage quota exceeded")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not save")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleExtKVDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if code, msg := s.extKVGuard(r, id); code != 0 {
		writeError(w, code, msg)
		return
	}
	key, ok := extKVKey(r)
	if !ok {
		writeErrorCode(w, http.StatusBadRequest, "validation", "invalid key")
		return
	}
	if !extKVWrites.allow() {
		writeError(w, http.StatusTooManyRequests, "storage is being written too quickly; slow down")
		return
	}
	if err := s.st.ExtKVDelete(id, key); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// tokenBucket is a minimal refilling limiter used to cap ext_kv write throughput.
type tokenBucket struct {
	mu     sync.Mutex
	tokens float64
	rate   float64 // tokens per second
	burst  float64
	last   time.Time
}

func newTokenBucket(rate, burst float64) *tokenBucket {
	return &tokenBucket{tokens: burst, rate: rate, burst: burst, last: time.Now()}
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens = math.Min(b.burst, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}
