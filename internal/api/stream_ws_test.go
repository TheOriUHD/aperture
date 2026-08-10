package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestStreamWSTokenAuth pins the live-WebSocket authorization the Home Assistant card
// relies on: the `token` query param may be a short-lived stream token OR a scoped
// media:live API token, and camera access is still enforced (owner ∩ grants).
func TestStreamWSTokenAuth(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: store.RoleAdmin}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam2", Name: "2"}))

	apiTok := func(scopes, cams []string) string {
		_, tok, err := s.st.CreateAPIToken(admin.ID, "t", 0, scopes, cams)
		mustDo(t, err)
		return tok
	}
	authOK := func(token string) bool {
		r := httptest.NewRequest("GET", "/api/v1/cameras/cam1/ws", nil)
		_, ok := s.authorizeStreamWS(r, "cam1", token)
		return ok
	}

	if !authOK(apiTok([]string{ScopeMediaLive}, []string{"cam1"})) {
		t.Error("a media:live token for cam1 should authorize the socket")
	}
	if authOK(apiTok([]string{ScopeMediaRead}, []string{"cam1"})) {
		t.Error("media:read (not live) must NOT authorize the live socket")
	}
	if authOK(apiTok([]string{ScopeMediaLive}, []string{"cam2"})) {
		t.Error("a token scoped to cam2 must NOT open cam1 (camera ACL still enforced)")
	}
	if authOK("not-a-real-token") || authOK("") {
		t.Error("an invalid/empty token must not authorize")
	}

	// The first-party short-lived stream token still authorizes.
	st, err := s.am.StreamToken("cam1", time.Minute)
	mustDo(t, err)
	if !authOK(st) {
		t.Error("a valid stream token must authorize")
	}
	// ...but a stream token for a different camera does not.
	st2, err := s.am.StreamToken("cam2", time.Minute)
	mustDo(t, err)
	if authOK(st2) {
		t.Error("a stream token for cam2 must not open cam1")
	}
}
