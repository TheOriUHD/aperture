package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestLeaseAPICreateReleaseAndScope covers T2.8's lease API: create returns a
// lease that actually protects the window, media:read is required for tokens,
// and release removes it.
func TestLeaseAPICreateReleaseAndScope(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	// Create a lease (admin session).
	rec := s.reqBody("POST", "/api/v1/cameras/cam1/lease", adminC, strings.NewReader(`{"fromTs":1000,"toTs":5000,"ttlSec":600}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create lease: got %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	var l store.Lease
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &l))
	if l.ID == "" || l.CameraID != "cam1" {
		t.Fatalf("lease payload = %+v", l)
	}
	// It protects the window right now.
	if held, _ := s.st.LeaseOverlaps("cam1", 2000, 3000, l.ExpiresAt-1); !held {
		t.Error("created lease does not protect its window")
	}

	// Invalid window → 400.
	if rec := s.reqBody("POST", "/api/v1/cameras/cam1/lease", adminC, strings.NewReader(`{"fromTs":5000,"toTs":1000}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("inverted window: got %d, want 400", rec.Code)
	}

	// Release it → 204, and it no longer protects.
	if rec := s.reqAs("DELETE", "/api/v1/cameras/cam1/lease/"+l.ID, adminC); rec.Code != http.StatusNoContent {
		t.Fatalf("release lease: got %d, want 204", rec.Code)
	}
	if held, _ := s.st.LeaseOverlaps("cam1", 2000, 3000, l.ExpiresAt-1); held {
		t.Error("lease still protects after release")
	}

	// A token without media:read is refused.
	_, noScope, err := s.st.CreateAPIToken(admin.ID, "noscope", 0, nil, []string{"cam1"})
	mustDo(t, err)
	if rec := s.reqBody("POST", "/api/v1/cameras/cam1/lease", cred{"token", bearerCred(noScope)}, strings.NewReader(`{"fromTs":1,"toTs":2}`)); rec.Code != http.StatusForbidden {
		t.Errorf("token without media:read: got %d, want 403", rec.Code)
	}
	// With media:read + camera grant it works.
	_, withScope, err := s.st.CreateAPIToken(admin.ID, "media", 0, []string{ScopeMediaRead}, []string{"cam1"})
	mustDo(t, err)
	if rec := s.reqBody("POST", "/api/v1/cameras/cam1/lease", cred{"token", bearerCred(withScope)}, strings.NewReader(`{"fromTs":1,"toTs":2}`)); rec.Code != http.StatusCreated {
		t.Errorf("token with media:read: got %d, want 201", rec.Code)
	}
}
