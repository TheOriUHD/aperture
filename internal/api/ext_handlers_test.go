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
	return s, cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}
}

const uiManifest = `{"manifest":1,"id":"com.example.panel","name":"Panel","version":"1.0.0","permissions":{"data":{"events":"read"}},"ui":{"panels":[{"slot":"settings","entry":"ui/panel.html"}]}}`

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

func TestExtInstallRejectsTraversal(t *testing.T) {
	s, adminC := newExtAdmin(t)
	mustDo(t, s.st.SetSetting("ext.enabled", "1"))
	z := zipBundle(t, map[string]string{"manifest.json": uiManifest, "../evil.txt": "pwned"})
	if rec := do(s, uploadBundle(t, "/api/v1/ext/install", adminC, z)); rec.Code != 400 {
		t.Fatalf("path-traversal bundle: got %d, want 400", rec.Code)
	}
}
