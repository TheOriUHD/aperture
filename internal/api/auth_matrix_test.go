package api

import (
	"net/http"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestAuthMatrix locks in the current authorization behavior across every
// credential class and the three enforcement mechanisms the v1 authz layer will
// centralize: RequireAuth (401 when anonymous), RequireAdmin (403 for viewers),
// and the hand-called allowedCamera per-camera gate (403 on a camera the caller
// has no ACL for).
//
// The `cli` class matters most: the CLI bearer token authenticates as a
// synthetic admin — the highest-privilege credential in the system — and must be
// covered before Phase 1.4 rewrites bearer resolution. Phase 1.4 adds the scoped
// `token` class simply by appending a cred and rows here.
func TestAuthMatrix(t *testing.T) {
	s := newTestServer(t)

	admin := &store.User{ID: "admin1", Email: "admin@test", Name: "Admin", Role: "admin"}
	viewer := &store.User{ID: "viewer1", Email: "viewer@test", Name: "Viewer", Role: "viewer"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateUser(viewer))

	cam1 := &store.Camera{ID: "cam1", Name: "Allowed Cam"}
	cam2 := &store.Camera{ID: "cam2", Name: "Denied Cam"}
	mustDo(t, s.st.CreateCamera(cam1))
	mustDo(t, s.st.CreateCamera(cam2))
	mustDo(t, s.st.SetUserCameras(viewer.ID, []string{cam1.ID})) // viewer may see cam1 only

	anon := cred{"anon", nil}
	viewerC := cred{"viewer", cookieCred(sessionCookieFor(t, s.am, viewer))}
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}
	cliC := cred{"cli", bearerCred(testCLIToken)} // synthetic admin

	cases := []struct {
		method, path string
		c            cred
		want         int
	}{
		// RequireAuth — authed, non-admin, non-camera route.
		{"GET", "/api/cameras", anon, http.StatusUnauthorized},
		{"GET", "/api/cameras", viewerC, http.StatusOK},
		{"GET", "/api/cameras", adminC, http.StatusOK},
		{"GET", "/api/cameras", cliC, http.StatusOK},

		// RequireAdmin — users listing is admin-only.
		{"GET", "/api/users", anon, http.StatusUnauthorized},
		{"GET", "/api/users", viewerC, http.StatusForbidden},
		{"GET", "/api/users", adminC, http.StatusOK},
		{"GET", "/api/users", cliC, http.StatusOK}, // cli == synthetic admin

		// allowedCamera — per-camera ACL on a camera-scoped route.
		{"GET", "/api/cameras/cam1/schedule", anon, http.StatusUnauthorized},
		{"GET", "/api/cameras/cam1/schedule", viewerC, http.StatusOK},
		{"GET", "/api/cameras/cam2/schedule", viewerC, http.StatusForbidden},
		{"GET", "/api/cameras/cam2/schedule", adminC, http.StatusOK},
		{"GET", "/api/cameras/cam2/schedule", cliC, http.StatusOK}, // admin bypass
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path+"/"+tc.c.name, func(t *testing.T) {
			rec := s.reqAs(tc.method, tc.path, tc.c)
			if rec.Code != tc.want {
				t.Errorf("%s %s [%s]: got %d, want %d (body: %s)",
					tc.method, tc.path, tc.c.name, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
