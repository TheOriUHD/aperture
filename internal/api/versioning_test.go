package api

import (
	"net/http"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestV1AliasAndVersionHeaders covers T1.1: /api/v1 is a true alias of the
// unversioned /api (same handlers, same authz), every API response carries the
// contract version, and only the unversioned alias is marked deprecated.
func TestV1AliasAndVersionHeaders(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}
	anon := cred{"anon", nil}

	// Both prefixes resolve the same handler with the same authorization.
	for _, prefix := range []string{"/api", "/api/v1"} {
		if rec := s.reqAs("GET", prefix+"/cameras", anon); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s/cameras anon: got %d, want 401", prefix, rec.Code)
		}
		rec := s.reqAs("GET", prefix+"/cameras", adminC)
		if rec.Code != http.StatusOK {
			t.Errorf("%s/cameras admin: got %d, want 200", prefix, rec.Code)
		}
		if v := rec.Header().Get("X-Aperture-Api-Version"); v != "v1" {
			t.Errorf("%s/cameras: X-Aperture-Api-Version = %q, want v1", prefix, v)
		}
	}

	// The unversioned alias is flagged deprecated with a successor link; v1 is not.
	legacy := s.reqAs("GET", "/api/cameras", adminC)
	if legacy.Header().Get("Deprecation") != "true" {
		t.Errorf("/api should carry Deprecation: true")
	}
	if legacy.Header().Get("Link") == "" {
		t.Errorf("/api should carry a successor-version Link")
	}
	if v1 := s.reqAs("GET", "/api/v1/cameras", adminC); v1.Header().Get("Deprecation") != "" {
		t.Errorf("/api/v1 must not be marked deprecated")
	}
}
