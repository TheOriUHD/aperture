package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

func zipBundle(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func uploadBundle(t *testing.T, path string, c cred, zipBytes []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("bundle", "bundle.zip")
	_, _ = fw.Write(zipBytes)
	_ = mw.Close()
	r := httptest.NewRequest("POST", path, &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	c.apply(r)
	return r
}

func do(s *Server, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}

func newExtAdmin(t *testing.T) (*Server, cred) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	return s, cred{"admin", cookieCred(sessionCookieFor(t, s, admin))}
}

// A pure-UI panel with a NON-camera scope: under the opt-in camera model it needs
// no camera request and gets no camera ceremony at install.
const uiManifest = `{"manifest":1,"id":"com.example.panel","name":"Panel","version":"1.0.0","permissions":{"system":"read"},"ui":{"panels":[{"slot":"settings","entry":"ui/panel.html"}]}}`

func TestExtInstallLifecycle(t *testing.T) {
	s, adminC := newExtAdmin(t)

	// Install is blocked until the gate is enabled.
	if rec := do(s, uploadBundle(t, "/api/v1/ext/install", adminC, zipBundle(t, map[string]string{"manifest.json": uiManifest}))); rec.Code != 403 {
		t.Fatalf("install before gate: got %d, want 403", rec.Code)
	}
	// Gate: wrong phrase → 400; correct → 200 and DD-FREEZE fires.
	if rec := s.reqBody("POST", "/api/v1/ext/gate", adminC, strings.NewReader(`{"confirm":"nope"}`)); rec.Code != 400 {
		t.Fatalf("gate wrong phrase: got %d, want 400", rec.Code)
	}
	if rec := s.reqBody("POST", "/api/v1/ext/gate", adminC, strings.NewReader(`{"confirm":"enable extensions"}`)); rec.Code != 200 {
		t.Fatalf("gate: got %d, want 200", rec.Code)
	}
	if v, _ := s.st.GetSetting("api.v1.frozen_at"); v == "" {
		t.Fatal("the gate must fire DD-FREEZE (api.v1.frozen_at)")
	}

	// Stage: returns consent metadata, does not commit.
	rec := do(s, uploadBundle(t, "/api/v1/ext/install", adminC, zipBundle(t, map[string]string{"manifest.json": uiManifest, "ui/panel.html": "<html>hi</html>"})))
	if rec.Code != 200 {
		t.Fatalf("install stage: got %d (%s)", rec.Code, rec.Body.String())
	}
	var stage struct {
		StagingID string   `json:"stagingId"`
		Consent   []string `json:"consent"`
	}
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &stage))
	if stage.StagingID == "" || len(stage.Consent) == 0 {
		t.Fatalf("expected stagingId + consent, got: %s", rec.Body.String())
	}

	// Confirm: commits.
	if rec := s.reqBody("POST", "/api/v1/ext/install/confirm", adminC, strings.NewReader(`{"stagingId":"`+stage.StagingID+`"}`)); rec.Code != 201 {
		t.Fatalf("confirm: got %d (%s)", rec.Code, rec.Body.String())
	}
	ex, err := s.st.GetExtension("com.example.panel")
	mustDo(t, err)
	if !ex.Enabled || ex.ExtUID < extUIDBase || ex.InstalledBy != "a1" {
		t.Fatalf("row wrong: %+v", ex)
	}
	if _, err := os.Stat(s.extDir("com.example.panel") + "/ui/panel.html"); err != nil {
		t.Fatalf("bundle files not extracted: %v", err)
	}

	// List.
	rec = s.reqAs("GET", "/api/v1/ext", adminC)
	var list []map[string]any
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &list))
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}

	// Disable → enable.
	if rec := s.reqBody("POST", "/api/v1/ext/com.example.panel/disable", adminC, nil); rec.Code != 200 {
		t.Fatalf("disable: %d", rec.Code)
	}
	if e, _ := s.st.GetExtension("com.example.panel"); e.Enabled {
		t.Fatal("should be disabled")
	}
	if rec := s.reqBody("POST", "/api/v1/ext/com.example.panel/enable", adminC, nil); rec.Code != 200 {
		t.Fatalf("enable: %d", rec.Code)
	}

	// Uninstall: row + files gone.
	if rec := s.reqAs("DELETE", "/api/v1/ext/com.example.panel", adminC); rec.Code != 204 {
		t.Fatalf("uninstall: %d", rec.Code)
	}
	if _, err := s.st.GetExtension("com.example.panel"); err == nil {
		t.Fatal("row should be gone after uninstall")
	}
	if _, err := os.Stat(s.extDir("com.example.panel")); !os.IsNotExist(err) {
		t.Fatal("files should be gone after uninstall")
	}
}

func TestExtAssetServing(t *testing.T) {
	s, _ := newExtAdmin(t)
	mustDo(t, s.st.SetSetting("ext.enabled", "1"))
	mustDo(t, s.st.CreateExtension(&store.Extension{ID: "com.x", Name: "X", Version: "1.0.0", Source: "sideload", Enabled: true, ManifestJSON: "{}"}))
	dir := s.extDir("com.x")
	mustDo(t, os.MkdirAll(dir+"/ui", 0o755))
	mustDo(t, os.WriteFile(dir+"/ui/panel.html", []byte("<h1>panel</h1>"), 0o644))
	mustDo(t, os.MkdirAll(dir+"/backend", 0o755))
	mustDo(t, os.WriteFile(dir+"/backend/main.ts", []byte("secret code"), 0o644))

	get := func(p string) *httptest.ResponseRecorder { return do(s, httptest.NewRequest("GET", p, nil)) }

	// UI asset served (unauthenticated — the sandbox iframe has no cookie) with CSP.
	rec := get("/ext/com.x/ui/panel.html")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "panel") {
		t.Fatalf("serve ui: got %d (%s)", rec.Code, rec.Body.String())
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "connect-src 'none'") {
		t.Fatalf("missing/weak CSP: %q", csp)
	}
	// Only the ui/ subtree — backend code and the manifest are never web-served.
	if rec := get("/ext/com.x/backend/main.ts"); rec.Code != 404 {
		t.Errorf("backend served: %d (must not be)", rec.Code)
	}
	if rec := get("/ext/com.x/manifest.json"); rec.Code != 404 {
		t.Errorf("manifest served: %d (must not be)", rec.Code)
	}
	// Disabled extension → 404; unknown extension → 404.
	mustDo(t, s.st.SetExtensionEnabled("com.x", false))
	if rec := get("/ext/com.x/ui/panel.html"); rec.Code != 404 {
		t.Errorf("disabled extension served: %d", rec.Code)
	}
	if rec := get("/ext/nope/ui/x.html"); rec.Code != 404 {
		t.Errorf("unknown extension served: %d", rec.Code)
	}
}

func TestExtGateDisableGuardsAndLatch(t *testing.T) {
	s, adminC := newExtAdmin(t)
	mustDo(t, s.st.SetSetting("ext.enabled", "1"))
	mustDo(t, s.st.CreateExtension(&store.Extension{ID: "com.x", Name: "X", Version: "1.0.0", Source: "sideload", Enabled: true, ManifestJSON: "{}"}))
	dir := s.extDir("com.x")
	mustDo(t, os.MkdirAll(dir+"/ui", 0o755))
	mustDo(t, os.WriteFile(dir+"/ui/p.html", []byte("x"), 0o644))
	s.freezeV1()
	frozen, _ := s.st.GetSetting("api.v1.frozen_at")
	if frozen == "" {
		t.Fatal("precondition: freeze should be set")
	}

	// Gate on: asset serves.
	if rec := do(s, httptest.NewRequest("GET", "/ext/com.x/ui/p.html", nil)); rec.Code != 200 {
		t.Fatalf("asset with gate on: %d", rec.Code)
	}

	// Disable the gate.
	if rec := s.reqAs("DELETE", "/api/v1/ext/gate", adminC); rec.Code != 200 {
		t.Fatalf("disable gate: %d", rec.Code)
	}
	if v, _ := s.st.GetSetting("ext.enabled"); v == "1" {
		t.Fatal("gate should be off after DELETE")
	}
	// The freeze is a one-way latch — disabling the gate must NOT clear it.
	if v, _ := s.st.GetSetting("api.v1.frozen_at"); v != frozen {
		t.Fatalf("disable cleared frozen_at (was %q, now %q) — it must not", frozen, v)
	}
	// With the gate off both extension surfaces refuse.
	if rec := do(s, httptest.NewRequest("GET", "/ext/com.x/ui/p.html", nil)); rec.Code != 404 {
		t.Errorf("asset with gate off: %d, want 404", rec.Code)
	}
	if rec := s.reqBody("POST", "/api/v1/ext/com.x/broker", adminC, strings.NewReader(`{"method":"GET","path":"/cameras"}`)); rec.Code != 404 {
		t.Errorf("broker with gate off: %d, want 404", rec.Code)
	}

	// Re-enable restores it (the row was left in place).
	if rec := s.reqBody("POST", "/api/v1/ext/gate", adminC, strings.NewReader(`{"confirm":"enable extensions"}`)); rec.Code != 200 {
		t.Fatalf("re-enable gate: %d", rec.Code)
	}
	if rec := do(s, httptest.NewRequest("GET", "/ext/com.x/ui/p.html", nil)); rec.Code != 200 {
		t.Errorf("asset after re-enable: %d, want 200", rec.Code)
	}
}

func TestExtInstallRejectsTraversal(t *testing.T) {
	s, adminC := newExtAdmin(t)
	mustDo(t, s.st.SetSetting("ext.enabled", "1"))
	z := zipBundle(t, map[string]string{"manifest.json": uiManifest, "../evil.txt": "pwned"})
	if rec := do(s, uploadBundle(t, "/api/v1/ext/install", adminC, z)); rec.Code != 400 {
		t.Fatalf("path-traversal bundle: got %d, want 400", rec.Code)
	}
}
