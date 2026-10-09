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
		if claims := m.sessionFromRequest(w, r); claims != nil {
			r = r.WithContext(context.WithValue(r.Context(), claimsKey, claims))
		} else if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			if claims := m.CLISession(bearer); claims != nil {
				r = r.WithContext(context.WithValue(r.Context(), claimsKey, claims))
			} else if claims := m.APIToken(bearer); claims != nil {
				r = r.WithContext(context.WithValue(r.Context(), claimsKey, claims))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// sessionFromRequest resolves the session cookie, if any, through the store-backed
// resolver. It runs before the handler writes anything, so the resolver may still
// set a renewed cookie on w.
func (m *Manager) sessionFromRequest(w http.ResponseWriter, r *http.Request) *SessionClaims {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" || m.session == nil {
		return nil
	}
	claims := m.session(w, r, c.Value)
	if claims != nil {
		claims.Class = ClassSession
	}
	return claims
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

// WithClaims returns a context carrying the given session claims. Used by the
// Context middleware and by tests that drive handlers directly.
func WithClaims(ctx context.Context, c *SessionClaims) context.Context {
	return context.WithValue(ctx, claimsKey, c)
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
