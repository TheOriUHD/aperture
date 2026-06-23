package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type ctxKey int

const claimsKey ctxKey = iota

// Context injects the verified session (if any) into the request context so
// downstream handlers can read it. It never rejects requests itself. A valid
// CLI bearer token authenticates as a synthetic admin (for nvrctl).
func (m *Manager) Context(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if claims, err := m.SessionFromRequest(r); err == nil {
			r = r.WithContext(context.WithValue(r.Context(), claimsKey, claims))
		} else if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			if claims := m.CLISession(bearer); claims != nil {
				r = r.WithContext(context.WithValue(r.Context(), claimsKey, claims))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth rejects unauthenticated requests with 401.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if FromContext(r.Context()) == nil {
			writeErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin rejects non-admin requests with 401/403.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := FromContext(r.Context())
		if c == nil {
			writeErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if c.Role != "admin" {
			writeErr(w, http.StatusForbidden, "administrator access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// FromContext returns the session claims stored on the context, or nil.
func FromContext(ctx context.Context) *SessionClaims {
	c, _ := ctx.Value(claimsKey).(*SessionClaims)
	return c
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
