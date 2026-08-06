package api

import (
	"net/http"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestMediaScopeEnforcement pins the media scope split (read vs live vs write)
// across the 3.2 routes for token credentials.
func TestMediaScopeEnforcement(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))

	mk := func(scopes []string) cred {
		_, tok, err := s.st.CreateAPIToken(admin.ID, "t", 0, scopes, []string{"cam1"})
		mustDo(t, err)
		return cred{"token", bearerCred(tok)}
	}
	none := mk(nil)
	rd := mk([]string{ScopeMediaRead})
	live := mk([]string{ScopeMediaLive})

	check := func(c cred, method, path string, want int) {
		if rec := s.reqAs(method, path, c); rec.Code != want {
			t.Errorf("%s %s → %d, want %d", method, path, rec.Code, want)
		}
	}
	// No media scope → denied on playback.
	check(none, "GET", "/api/v1/cameras/cam1/hls", http.StatusForbidden)
	// media:read → playback yes; live-stream token no (needs media:live).
	check(rd, "GET", "/api/v1/cameras/cam1/hls", http.StatusOK)
	check(rd, "GET", "/api/v1/cameras/cam1/stream-token", http.StatusForbidden)
	check(rd, "GET", "/api/v1/export", http.StatusOK)
	// media:live opens the live-stream token but not playback or the export list.
	check(live, "GET", "/api/v1/cameras/cam1/hls", http.StatusForbidden)
	check(live, "GET", "/api/v1/export", http.StatusForbidden)
}
