package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestErrorEnvelopeHasMachineCode covers T1.8: every v1 error carries a stable
// machine-readable `code` (default from the status, refined for camera/scope
// denials) alongside the human `error` prose the web app still reads.
func TestErrorEnvelopeHasMachineCode(t *testing.T) {
	s := newTestServer(t)
	viewer := &store.User{ID: "v1", Email: "v@t", Name: "V", Role: "viewer"}
	mustDo(t, s.st.CreateUser(viewer))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "Own"}))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam2", Name: "Other"}))
	mustDo(t, s.st.SetUserCameras(viewer.ID, []string{"cam1"}))
	viewerC := cred{"viewer", cookieCred(sessionCookieFor(t, s, viewer))}
	anon := cred{"anon", nil}

	cases := []struct {
		name, method, path string
		c                  cred
		wantStatus         int
		wantCode           string
	}{
		{"unauthenticated", "GET", "/api/v1/cameras", anon, http.StatusUnauthorized, "unauthenticated"},
		{"forbidden-admin-route", "GET", "/api/v1/users", viewerC, http.StatusForbidden, "forbidden"},
		{"forbidden-camera", "GET", "/api/v1/cameras/cam2/schedule", viewerC, http.StatusForbidden, "forbidden.camera"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := s.reqAs(tc.method, tc.path, tc.c)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
			}
			if body["code"] != tc.wantCode {
				t.Errorf("code = %q, want %q", body["code"], tc.wantCode)
			}
			if body["error"] == "" {
				t.Errorf("error prose must remain present (web app reads it): %s", rec.Body.String())
			}
		})
	}
}
