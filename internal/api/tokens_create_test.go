package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestCreateTokenScopes pins the token-creation contract the web "New token" form
// relies on. It regression-guards the bug where the UI posted {name, expiresInDays}
// with no scopes: since A3 an empty-scope token is refused, so every creation 400'd
// and the form (which had no scope picker) could never succeed. The contract:
//   - no scopes            → 400, "at least one scope is required" (not 500, not "name required")
//   - blank name           → 400, "name required" (distinct from the scope/decode paths)
//   - unknown scope        → 400, "unknown scope(s): …"
//   - name + ≥1 good scope → 200 with the one-time plaintext secret
func TestCreateTokenScopes(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "u1", Email: "a@t", Name: "A", Role: store.RoleAdmin}
	mustDo(t, s.st.CreateUser(admin))
	sess := sessionCookieFor(t, s, admin)

	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/tokens", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: "aperture_session", Value: sess})
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec
	}

	if rec := post(`{"name":"x","expiresInDays":0}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "at least one scope") {
		t.Errorf("no-scope token: want 400 scope-required, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(`{"name":"  ","scopes":["media:live"]}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "name required") {
		t.Errorf("blank name: want 400 name-required, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(`{"name":"x","scopes":["media:bogus"]}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "unknown scope") {
		t.Errorf("bad scope: want 400 unknown-scope, got %d %s", rec.Code, rec.Body.String())
	}

	rec := post(`{"name":"ha-live-card","expiresInDays":30,"scopes":["media:live"],"cameraGrants":["cam1"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Secret string `json:"secret"`
		Token  struct {
			Name string `json:"name"`
		} `json:"token"`
	}
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &out))
	if !strings.HasPrefix(out.Secret, store.APITokenPrefix) {
		t.Errorf("valid token: secret should start with %q, got %q", store.APITokenPrefix, out.Secret)
	}
	if out.Token.Name != "ha-live-card" {
		t.Errorf("valid token: name = %q, want ha-live-card", out.Token.Name)
	}
}
