package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestTokenCreationEnforcesScopesAndFreezes covers the Phase 3 finalization: PAT
// minting is un-gated now that scopes are enforced, requires at least one scope
// (A3 — no dead empty-scope tokens), and the first token freezes v1 (DD-FREEZE).
func TestTokenCreationEnforcesScopesAndFreezes(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	// Empty scopes → 400 (a dead token, refused).
	rec := s.reqBody("POST", "/api/v1/tokens", adminC, strings.NewReader(`{"name":"dead"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty-scope token: got %d, want 400", rec.Code)
	}
	// v1 not yet frozen.
	if v, _ := s.st.GetSetting("api.v1.frozen_at"); v != "" {
		t.Fatalf("v1 froze on a rejected creation: %q", v)
	}

	// A scoped token is minted and returns its secret once; v1 freezes.
	rec = s.reqBody("POST", "/api/v1/tokens", adminC, strings.NewReader(`{"name":"ext","scopes":["cameras:read"]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped token: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Secret string `json:"secret"`
	}
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &body))
	if body.Secret == "" {
		t.Error("token secret not returned")
	}
	if v, _ := s.st.GetSetting("api.v1.frozen_at"); v == "" {
		t.Error("first PAT should have frozen v1 (api.v1.frozen_at)")
	}
}

// TestScopedTokenSessionOnlyAndGrants covers T1.4: a class=token credential is
// rejected from session-only operations even when its owner is an admin, and its
// camera grants narrow access below the owner (never above).
func TestScopedTokenSessionOnlyAndGrants(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam2", Name: "2"}))

	// Admin-owned token, granted only cam1, holding cameras:read + schedules:read
	// (Phase 3 scoped these routes; the camera grant then narrows WHICH cameras).
	_, plain, err := s.st.CreateAPIToken(admin.ID, "ext", 0, []string{ScopeCamerasRead, ScopeSchedulesRead}, []string{"cam1"})
	mustDo(t, err)
	tokenC := cred{"token", bearerCred(plain)}

	// Session-only: forbidden to a token despite the admin owner.
	for _, path := range []string{"/api/v1/system/backup", "/api/v1/users"} {
		if rec := s.reqAs("GET", path, tokenC); rec.Code != http.StatusForbidden {
			t.Errorf("token GET %s: got %d, want 403 (session-only)", path, rec.Code)
		}
	}
	if rec := s.reqAs("POST", "/api/v1/tokens", tokenC); rec.Code != http.StatusForbidden {
		t.Errorf("token minting a token: got %d, want 403 (session-only)", rec.Code)
	}

	// A route whose scope the token holds works (cameras:read → /cameras).
	if rec := s.reqAs("GET", "/api/v1/cameras", tokenC); rec.Code != http.StatusOK {
		t.Errorf("token GET /cameras: got %d, want 200", rec.Code)
	}

	// Camera grant narrows an admin owner: cam1 allowed, cam2 denied.
	if rec := s.reqAs("GET", "/api/v1/cameras/cam1/schedule", tokenC); rec.Code != http.StatusOK {
		t.Errorf("token granted cam1: got %d, want 200", rec.Code)
	}
	if rec := s.reqAs("GET", "/api/v1/cameras/cam2/schedule", tokenC); rec.Code != http.StatusForbidden {
		t.Errorf("token NOT granted cam2: got %d, want 403 (grant narrows even an admin owner)", rec.Code)
	}
}

// TestValidateScopes covers the token-creation scope validation.
func TestValidateScopes(t *testing.T) {
	if bad := validateScopes([]string{ScopeCamerasRead, ScopeMediaLive}); bad != "" {
		t.Errorf("known scopes rejected: %q", bad)
	}
	if bad := validateScopes([]string{ScopeCamerasRead, "bogus:scope"}); bad != "bogus:scope" {
		t.Errorf("unknown scope not flagged: got %q, want bogus:scope", bad)
	}
}
