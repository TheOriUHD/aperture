package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/store"
)

// TestWithIdempotency covers T1.9: a repeated create with the same
// Idempotency-Key replays the first response without re-running the handler; a
// different key or no key runs it again.
func TestWithIdempotency(t *testing.T) {
	s := newTestServer(t)
	mustDo(t, s.st.CreateUser(&store.User{ID: "u1", Email: "u@t", Name: "U", Role: "admin"}))

	calls := 0
	wrapped := s.withIdempotency(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeJSON(w, http.StatusOK, map[string]int{"n": calls})
	})
	req := func(key string) *http.Request {
		r := httptest.NewRequest("POST", "/api/v1/export", nil)
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		return r.WithContext(auth.WithClaims(r.Context(), &auth.SessionClaims{UserID: "u1", Role: "admin", Class: auth.ClassSession}))
	}

	r1 := httptest.NewRecorder()
	wrapped(r1, req("abc-123"))
	r2 := httptest.NewRecorder()
	wrapped(r2, req("abc-123"))

	if calls != 1 {
		t.Errorf("handler ran %d times for one key, want 1 (idempotent)", calls)
	}
	if r1.Body.String() != r2.Body.String() {
		t.Errorf("replay body differs: %q vs %q", r1.Body.String(), r2.Body.String())
	}
	if r2.Header().Get("Idempotent-Replay") != "true" {
		t.Errorf("replay not flagged")
	}

	r3 := httptest.NewRecorder()
	wrapped(r3, req("different"))
	if calls != 2 {
		t.Errorf("different key should run the handler: calls=%d, want 2", calls)
	}

	r4 := httptest.NewRecorder()
	wrapped(r4, req(""))
	if calls != 3 {
		t.Errorf("no key should always run: calls=%d, want 3", calls)
	}
}
