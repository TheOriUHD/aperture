package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestVersionAndCapabilities covers T1.2: /version is public and carries the API
// contract version + schema; /capabilities requires auth and reports subsystem
// topology + feature flags.
func TestVersionAndCapabilities(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	// /version is public (pre-auth negotiation).
	rec := s.reqAs("GET", "/api/v1/version", cred{"anon", nil})
	if rec.Code != http.StatusOK {
		t.Fatalf("/version anon: got %d, want 200", rec.Code)
	}
	var ver map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &ver); err != nil {
		t.Fatalf("decode /version: %v", err)
	}
	if _, ok := ver["apiVersions"]; !ok {
		t.Errorf("/version missing apiVersions: %v", ver)
	}
	if _, ok := ver["schemaVersion"]; !ok {
		t.Errorf("/version missing schemaVersion: %v", ver)
	}

	// /capabilities requires authentication.
	if rec := s.reqAs("GET", "/api/v1/capabilities", cred{"anon", nil}); rec.Code != http.StatusUnauthorized {
		t.Errorf("/capabilities anon: got %d, want 401", rec.Code)
	}
	rec = s.reqAs("GET", "/api/v1/capabilities", adminC)
	if rec.Code != http.StatusOK {
		t.Fatalf("/capabilities admin: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var caps map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &caps); err != nil {
		t.Fatalf("decode /capabilities: %v", err)
	}
	subs, ok := caps["subsystems"].(map[string]any)
	if !ok {
		t.Fatalf("/capabilities missing subsystems object: %v", caps)
	}
	// A fresh store has detection off, no HA, no site → all these are false.
	for _, k := range []string{"detection", "homeAssistant", "onvif", "weather", "audio"} {
		if v, ok := subs[k].(bool); !ok || v {
			t.Errorf("subsystem %q: got %v, want false on a fresh store", k, subs[k])
		}
	}
	if _, ok := caps["features"]; !ok {
		t.Errorf("/capabilities missing features")
	}
}
