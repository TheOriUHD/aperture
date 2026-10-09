package api

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// newBrokerTest builds a server with two cameras, an admin and a viewer scoped to
// cam1, and two extensions installed by the admin: one granting all cameras +
// cameras:read, one granting the admin-gated site:write.
func newBrokerTest(t *testing.T) (*Server, cred, cred) {
	t.Helper()
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	viewer := &store.User{ID: "v1", Email: "v@t", Name: "V", Role: "viewer"}
	mustDo(t, s.st.CreateUser(viewer))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "C1"}))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam2", Name: "C2"}))
	mustDo(t, s.st.SetUserCameras(viewer.ID, []string{"cam1"})) // viewer sees cam1 only
	mustDo(t, s.st.SetSetting("ext.enabled", "1"))              // the extension subsystem is on

	mustDo(t, s.st.CreateExtension(&store.Extension{
		ID: "ext.cams", Name: "Cams", Version: "1.0.0", Source: "sideload",
		Scopes: []string{"cameras:read"}, CameraGrants: []string{"cam1", "cam2"}, // all cameras
		Enabled: true, InstalledBy: admin.ID,
	}))
	mustDo(t, s.st.CreateExtension(&store.Extension{
		ID: "ext.site", Name: "Site", Version: "1.0.0", Source: "sideload",
		Scopes: []string{"site:write"}, Enabled: true, InstalledBy: admin.ID,
	}))
	return s,
		cred{"admin", cookieCred(sessionCookieFor(t, s, admin))},
		cred{"viewer", cookieCred(sessionCookieFor(t, s, viewer))}
}

func brokerCall(t *testing.T, s *Server, c cred, extID, method, path, origin string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"method":%q,"path":%q}`, method, path)
	r := httptest.NewRequest("POST", "/api/v1/ext/"+extID+"/broker", strings.NewReader(body))
	c.apply(r)
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}

// TestBrokerConfusedDeputy is the R1 matrix: effective authority is extension ∩
// calling session on camera, role, and scope — so a UI extension can never do
// more for a user than the user could unaided, nor more than its manifest.
func TestBrokerConfusedDeputy(t *testing.T) {
	s, adminC, viewerC := newBrokerTest(t)

	// Camera dimension: the extension grants all cameras, but the viewer only sees
	// cam1 — so cam2 is denied, cam1 allowed.
	if rec := brokerCall(t, s, viewerC, "ext.cams", "GET", "/cameras/cam2", ""); rec.Code != 403 {
		t.Errorf("viewer via all-cameras extension → cam2: got %d, want 403 (camera ∩)", rec.Code)
	}
	if rec := brokerCall(t, s, viewerC, "ext.cams", "GET", "/cameras/cam1", ""); rec.Code != 200 {
		t.Errorf("viewer → cam1 (in both): got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	// Role dimension: the extension holds the admin-gated site:write, but the
	// caller is a viewer — the admin gate uses the CALLER's role.
	if rec := brokerCall(t, s, viewerC, "ext.site", "PUT", "/system/site", ""); rec.Code != 403 {
		t.Errorf("viewer via site:write extension → PUT /system/site: got %d, want 403 (role ∩)", rec.Code)
	}

	// Scope dimension: an admin caller gets the EXTENSION's ceiling, not admin
	// authority — ext.cams has no events:read, so /events is refused even though
	// the admin could call it directly.
	if rec := brokerCall(t, s, adminC, "ext.cams", "GET", "/events", ""); rec.Code != 403 {
		t.Errorf("admin via cameras-only extension → /events: got %d, want 403 forbidden.scope", rec.Code)
	}
	// ...but the admin CAN reach cam2 through ext.cams (admin ACL ∩ grants = both).
	if rec := brokerCall(t, s, adminC, "ext.cams", "GET", "/cameras/cam2", ""); rec.Code != 200 {
		t.Errorf("admin → cam2 via all-cameras extension: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

func TestBrokerDisabledExtension(t *testing.T) {
	s, _, viewerC := newBrokerTest(t)
	mustDo(t, s.st.SetExtensionEnabled("ext.cams", false))
	if rec := brokerCall(t, s, viewerC, "ext.cams", "GET", "/cameras/cam1", ""); rec.Code != 404 {
		t.Errorf("disabled extension broker: got %d, want 404", rec.Code)
	}
}

// TestCSRFGuard covers R2+R5 on a session-cookie POST: present-and-foreign Origin
// is blocked, same-origin allowed, absent allowed (nvrctl/curl path).
func TestCSRFGuard(t *testing.T) {
	s, _, viewerC := newBrokerTest(t)
	// httptest.NewRequest sets Host = example.com.
	if rec := brokerCall(t, s, viewerC, "ext.cams", "GET", "/cameras/cam1", "http://evil.com"); rec.Code != 403 {
		t.Errorf("foreign Origin: got %d, want 403", rec.Code)
	}
	if rec := brokerCall(t, s, viewerC, "ext.cams", "GET", "/cameras/cam1", "http://example.com"); rec.Code != 200 {
		t.Errorf("same Origin: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if rec := brokerCall(t, s, viewerC, "ext.cams", "GET", "/cameras/cam1", ""); rec.Code != 200 {
		t.Errorf("absent Origin: got %d, want 200 (the nvrctl/curl path)", rec.Code)
	}
}
