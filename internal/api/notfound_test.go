package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAPIUnmatchedPathReturnsJSONNotFound pins the API-prefix 404 contract: an
// unmatched path under /api or /api/v1 must return the JSON error envelope with a
// machine-readable "not_found" code, NOT the SPA's 200 text/html. Third-party
// extensions decode every response as JSON (see @aperture/sdk), so HTML for a
// typo'd path would surface as a confusing parse error and violate the OpenAPI
// promise that errors are JSON. Genuine (non-API) app routes must still fall
// through to the SPA.
func TestAPIUnmatchedPathReturnsJSONNotFound(t *testing.T) {
	s := newTestServerNoRouter(t)
	// A deterministic SPA shell so we can prove genuine app routes still reach it
	// (independent of whatever the embedded bundle contains in the test binary).
	webdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webdir, "index.html"),
		[]byte("<!doctype html><title>app shell</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.cfg.WebDir = webdir
	s.router = s.routes()

	// Unmatched paths under BOTH mounts, across methods → JSON 404 / not_found.
	cases := []struct{ method, path string }{
		{"GET", "/api/v1/nope/typo"},
		{"GET", "/api/nope"}, // the unversioned alias
		{"GET", "/api/v1/cameras/anything/not-a-subroute"},
		{"POST", "/api/v1/definitely-not-a-route"}, // non-GET is still JSON, not HTML
		{"DELETE", "/api/v1/nope"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404", c.method, c.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("%s %s: content-type = %q, want application/json", c.method, c.path, ct)
		}
		var body struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("%s %s: body is not JSON (%q): %v", c.method, c.path, rec.Body.String(), err)
			continue
		}
		if body.Code != "not_found" {
			t.Errorf("%s %s: code = %q, want not_found", c.method, c.path, body.Code)
		}
		if body.Error == "" {
			t.Errorf("%s %s: expected a human error message alongside the code", c.method, c.path)
		}
		if strings.Contains(rec.Body.String(), "<!doctype") || strings.Contains(rec.Body.String(), "<title>") {
			t.Errorf("%s %s: body looks like SPA HTML, not a JSON error: %q", c.method, c.path, rec.Body.String())
		}
	}

	// A genuine (non-API) app route still falls through to the SPA, unchanged.
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/dashboard", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("app route /dashboard: status = %d, want 200 (SPA fallthrough)", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("app route /dashboard: content-type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "app shell") {
		t.Errorf("app route /dashboard: did not serve the SPA index.html: %q", rec.Body.String())
	}
}

// TestAPIMethodNotAllowedReturnsJSON pins the 405 half of the contract: a wrong
// method on an EXISTING API path returns the JSON error envelope with code
// "method_not_allowed", not chi's default empty-body 405.
func TestAPIMethodNotAllowedReturnsJSON(t *testing.T) {
	s := newTestServer(t)
	// GET /system/health exists (public); POST does not — a method mismatch, not a
	// missing path. Checked on both mounts.
	for _, p := range []string{"/api/v1/system/health", "/api/system/health"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("POST", p, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: status = %d, want 405", p, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("POST %s: content-type = %q, want application/json", p, ct)
		}
		var body struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("POST %s: body is not JSON (%q): %v", p, rec.Body.String(), err)
			continue
		}
		if body.Code != "method_not_allowed" {
			t.Errorf("POST %s: code = %q, want method_not_allowed", p, body.Code)
		}
		if body.Error == "" {
			t.Errorf("POST %s: expected a human error message", p)
		}
	}
}
