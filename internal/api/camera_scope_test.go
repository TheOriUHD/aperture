package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestCameraScopeDenied pins the per-camera authorization for every camera-scoped
// route BEFORE the hand-called allowedCamera guards are removed from the handler
// bodies (the enforce layer now carries the check). For each route an anonymous
// caller is 401 and a viewer without the camera's ACL is 403 — the
// security-critical denial the guard exists to provide.
func TestCameraScopeDenied(t *testing.T) {
	s := newTestServer(t)
	viewer := &store.User{ID: "v1", Email: "v@t", Name: "V", Role: "viewer"}
	mustDo(t, s.st.CreateUser(viewer))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "Own"}))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam2", Name: "Other"}))
	mustDo(t, s.st.SetUserCameras(viewer.ID, []string{"cam1"})) // viewer may see cam1 only
	viewerC := cred{"viewer", cookieCred(sessionCookieFor(t, s, viewer))}
	anon := cred{"anon", nil}

	// Every route scoped to a camera the viewer cannot see (cam2).
	routes := []struct{ method, path string }{
		{"GET", "/api/v1/cameras/cam2"},
		{"GET", "/api/v1/cameras/cam2/stream-token"},
		{"GET", "/api/v1/cameras/cam2/snapshot.jpg"},
		{"GET", "/api/v1/cameras/cam2/frame"},
		{"GET", "/api/v1/cameras/cam2/segments"},
		{"GET", "/api/v1/cameras/cam2/events"},
		{"GET", "/api/v1/cameras/cam2/hls"},
		{"GET", "/api/v1/cameras/cam2/seg/x.ts"},
		{"DELETE", "/api/v1/cameras/cam2/recordings"},
		{"GET", "/api/v1/cameras/cam2/schedule"},
		{"GET", "/api/v1/cameras/cam2/zones"},
	}
	for _, rt := range routes {
		t.Run("anon "+rt.method+" "+rt.path, func(t *testing.T) {
			if rec := s.reqAs(rt.method, rt.path, anon); rec.Code != http.StatusUnauthorized {
				t.Errorf("got %d, want 401", rec.Code)
			}
		})
		t.Run("viewer-denied "+rt.method+" "+rt.path, func(t *testing.T) {
			if rec := s.reqAs(rt.method, rt.path, viewerC); rec.Code != http.StatusForbidden {
				t.Errorf("got %d, want 403 (viewer must not reach a camera outside its ACL)", rec.Code)
			}
		})
	}
}

// TestListCamerasFilteredForViewer pins the list-filter camera-scoping: a viewer
// sees only cameras in its ACL, never others. (handleListCameras keeps its
// allowedCameraSet call — a filter, not a gate — so this locks that behavior.)
func TestListCamerasFilteredForViewer(t *testing.T) {
	s := newTestServer(t)
	viewer := &store.User{ID: "v1", Email: "v@t", Name: "V", Role: "viewer"}
	mustDo(t, s.st.CreateUser(viewer))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "Own"}))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam2", Name: "Other"}))
	mustDo(t, s.st.SetUserCameras(viewer.ID, []string{"cam1"}))
	viewerC := cred{"viewer", cookieCred(sessionCookieFor(t, s, viewer))}

	body := s.reqAs("GET", "/api/v1/cameras", viewerC).Body.String()
	if !strings.Contains(body, "cam1") {
		t.Errorf("viewer's camera list should include cam1: %s", body)
	}
	if strings.Contains(body, "cam2") {
		t.Errorf("viewer's camera list must NOT include cam2 (unfiltered): %s", body)
	}
}
