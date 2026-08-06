package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestOpenAPIGeneratedFromRegistry covers 5.1: the spec is generated from the
// route registry, so scopes-per-operation match what the enforce layer checks.
func TestOpenAPIGeneratedFromRegistry(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	rec := s.reqAs("GET", "/api/v1/openapi.json", adminC)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET openapi.json: got %d, want 200", rec.Code)
	}
	var doc map[string]any
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &doc))

	if doc["openapi"] != "3.0.3" {
		t.Errorf("openapi = %v, want 3.0.3", doc["openapi"])
	}
	paths, _ := doc["paths"].(map[string]any)
	if paths == nil {
		t.Fatal("no paths in spec")
	}
	op := func(path, method string) map[string]any {
		p, _ := paths[path].(map[string]any)
		o, _ := p[method].(map[string]any)
		return o
	}

	// A scoped operation lists exactly the scope the enforce layer requires.
	events := op("/events", "get")
	if events == nil {
		t.Fatal("/events GET missing from spec")
	}
	scopes, _ := events["x-required-scopes"].([]any)
	if len(scopes) != 1 || scopes[0] != "events:read" {
		t.Errorf("/events scopes = %v, want [events:read]", events["x-required-scopes"])
	}
	// A config write documents the If-Match preconditions.
	putCam := op("/cameras/{id}", "put")
	if putCam == nil || putCam["responses"].(map[string]any)["428"] == nil {
		t.Errorf("PUT /cameras/{id} should document 428 (If-Match required)")
	}
	// A public route carries no security requirement.
	if h := op("/system/health", "get"); h == nil || h["security"] != nil {
		t.Errorf("/system/health should be public (no security), got %v", h["security"])
	}
	// A session-only route is flagged.
	if b := op("/system/backup", "get"); b == nil || b["x-session-only"] != true {
		t.Errorf("/system/backup should be x-session-only")
	}
	// The shared Error schema is present.
	if doc["components"].(map[string]any)["schemas"].(map[string]any)["Error"] == nil {
		t.Error("components.schemas.Error missing")
	}
}
