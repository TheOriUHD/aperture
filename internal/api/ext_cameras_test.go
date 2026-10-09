package api

import (
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/ext"
	"github.com/TheOriUHD/aperture/internal/store"
)

func strsEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func ptr(s ...string) *[]string { v := append([]string{}, s...); return &v }

// TestResolveCameraGrants pins the ceiling/narrowing rules: the manifest is a
// request the user can only narrow, empty-selection is rejected (empty grants would
// mean ALL), and nil means "as requested".
func TestResolveCameraGrants(t *testing.T) {
	all := ext.Derived{RequestsCameras: true, CameraGrants: nil}                    // requested all cameras
	specific := ext.Derived{RequestsCameras: true, CameraGrants: []string{"a", "b"}} // requested a, b
	noCam := ext.Derived{RequestsCameras: false}

	// nil → the manifest default.
	if g, err := resolveCameraGrants(all, nil); err != nil || g != nil {
		t.Errorf("all/nil = %v,%v; want nil (all)", g, err)
	}
	if g, err := resolveCameraGrants(specific, nil); err != nil || !strsEq(g, []string{"a", "b"}) {
		t.Errorf("specific/nil = %v,%v; want [a b]", g, err)
	}
	// empty non-nil → rejected for a camera-requesting extension (empty == ALL).
	if _, err := resolveCameraGrants(all, &[]string{}); err == nil {
		t.Error("all/[] should be rejected")
	}
	if _, err := resolveCameraGrants(specific, &[]string{}); err == nil {
		t.Error("specific/[] should be rejected")
	}
	// a specific request is a ceiling: a subset is allowed, an outsider is not.
	if g, err := resolveCameraGrants(specific, ptr("a")); err != nil || !strsEq(g, []string{"a"}) {
		t.Errorf("specific narrow = %v,%v; want [a]", g, err)
	}
	if _, err := resolveCameraGrants(specific, ptr("a", "z")); err == nil {
		t.Error("specific with an unrequested camera should be rejected")
	}
	// request-all accepts any explicit subset (runtime bounds it by installer ∩).
	if g, err := resolveCameraGrants(all, ptr("x")); err != nil || !strsEq(g, []string{"x"}) {
		t.Errorf("all narrow = %v,%v; want [x]", g, err)
	}
	// no camera scope → the field is irrelevant, not an error.
	if _, err := resolveCameraGrants(noCam, &[]string{}); err != nil {
		t.Errorf("no-camera ext with [] should be fine: %v", err)
	}
}

// TestExtSetCameras drives the post-install edit endpoint: narrowing within the
// request succeeds and rewrites the grant; going outside it is refused.
func TestExtSetCameras(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "admin", Email: "a@x", Name: "Admin", Role: store.RoleAdmin}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.SetSetting("ext.enabled", "1"))
	manifest := `{"manifest":1,"id":"com.x","name":"X","version":"1.0.0","permissions":{"data":{"cameras":"read"},"cameras":["a","b","c"]},"ui":{"panels":[{"slot":"dashboard-card","entry":"ui/p.html","title":"P"}]}}`
	mustDo(t, s.st.CreateExtension(&store.Extension{
		ID: "com.x", Name: "X", Version: "1.0.0", Source: "sideload", Enabled: true,
		ManifestJSON: manifest, CameraGrants: []string{"a", "b", "c"}, InstalledBy: admin.ID,
	}))
	sess := cred{apply: cookieCred(sessionCookieFor(t, s, admin))}

	// Narrow to a subset → 200, grant rewritten.
	if rec := s.reqBody("PUT", "/api/v1/ext/com.x/cameras", sess, strings.NewReader(`{"cameras":["a"]}`)); rec.Code != 200 {
		t.Fatalf("narrow = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if ex, _ := s.st.GetExtension("com.x"); !strsEq(ex.CameraGrants, []string{"a"}) {
		t.Errorf("grants after narrow = %v, want [a]", ex.CameraGrants)
	}
	// A camera the manifest never requested → 400.
	if rec := s.reqBody("PUT", "/api/v1/ext/com.x/cameras", sess, strings.NewReader(`{"cameras":["a","z"]}`)); rec.Code != 400 {
		t.Errorf("out-of-request = %d, want 400", rec.Code)
	}
}
