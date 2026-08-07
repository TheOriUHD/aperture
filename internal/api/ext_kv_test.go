package api

import (
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestExtKVNamespaceIsolation is the security assertion for per-extension storage:
// a backend token may read/write only its OWN namespace, never another
// extension's, and an extension that never requested `storage` is refused outright.
func TestExtKVNamespaceIsolation(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "admin", Email: "a@x", Name: "Admin", Role: store.RoleAdmin}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.SetSetting("ext.enabled", "1"))

	withStorage := func(id string) string {
		return `{"manifest":1,"id":"` + id + `","name":"X","version":"1.0.0","permissions":{"storage":true},"backend":{"entry":"main.ts"}}`
	}
	mustDo(t, s.st.CreateExtension(&store.Extension{ID: "com.a", Name: "A", Version: "1.0.0", Source: "sideload", Enabled: true, ManifestJSON: withStorage("com.a"), InstalledBy: admin.ID}))
	mustDo(t, s.st.CreateExtension(&store.Extension{ID: "com.b", Name: "B", Version: "1.0.0", Source: "sideload", Enabled: true, ManifestJSON: withStorage("com.b"), InstalledBy: admin.ID}))
	// com.c did NOT request storage.
	mustDo(t, s.st.CreateExtension(&store.Extension{ID: "com.c", Name: "C", Version: "1.0.0", Source: "sideload", Enabled: true, ManifestJSON: `{"manifest":1,"id":"com.c","name":"C","version":"1.0.0","backend":{"entry":"main.ts"}}`, InstalledBy: admin.ID}))

	tokA, err := s.st.CreateExtensionToken(admin.ID, "com.a", "com.a", nil, nil)
	mustDo(t, err)
	tokC, err := s.st.CreateExtensionToken(admin.ID, "com.c", "com.c", nil, nil)
	mustDo(t, err)
	a := cred{name: "com.a", apply: bearerCred(tokA)}

	// com.a's token writes + reads its own namespace.
	if rec := s.reqBody("PUT", "/api/v1/ext/com.a/kv/ha_token", a, strings.NewReader(`{"value":"secret"}`)); rec.Code != 204 {
		t.Fatalf("own PUT = %d, want 204 (%s)", rec.Code, rec.Body)
	}
	if rec := s.reqAs("GET", "/api/v1/ext/com.a/kv/ha_token", a); rec.Code != 200 || !strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("own GET = %d %s", rec.Code, rec.Body)
	}

	// com.a's token CANNOT touch com.b's namespace — the confined-deputy check.
	if rec := s.reqAs("GET", "/api/v1/ext/com.b/kv/ha_token", a); rec.Code != 403 {
		t.Errorf("cross-extension GET = %d, want 403", rec.Code)
	}
	if rec := s.reqBody("PUT", "/api/v1/ext/com.b/kv/ha_token", a, strings.NewReader(`{"value":"x"}`)); rec.Code != 403 {
		t.Errorf("cross-extension PUT = %d, want 403", rec.Code)
	}

	// An extension that never requested storage is refused, even for its own id.
	if rec := s.reqAs("GET", "/api/v1/ext/com.c/kv/anything", cred{apply: bearerCred(tokC)}); rec.Code != 403 {
		t.Errorf("undeclared-storage GET = %d, want 403", rec.Code)
	}

	// An admin session (first-party) may reach any namespace — the host uses this
	// path to relay an admin's UI-panel storage calls under a trusted extId.
	adminSess := cred{apply: cookieCred(sessionCookieFor(t, s.am, admin))}
	if rec := s.reqAs("GET", "/api/v1/ext/com.a/kv/ha_token", adminSess); rec.Code != 200 {
		t.Errorf("admin session GET = %d, want 200", rec.Code)
	}

	// A VIEWER session must NOT read the extension's secrets — not by curl and not
	// via the panel bridge (which relays under the viewer's own session). This is
	// the hole the routes' `authed` spec would otherwise leave open.
	viewer := &store.User{ID: "viewer", Email: "v@x", Name: "Viewer", Role: store.RoleViewer}
	mustDo(t, s.st.CreateUser(viewer))
	viewerSess := cred{apply: cookieCred(sessionCookieFor(t, s.am, viewer))}
	if rec := s.reqAs("GET", "/api/v1/ext/com.a/kv/ha_token", viewerSess); rec.Code != 403 {
		t.Errorf("viewer session GET = %d, want 403 (storage is admin-only for humans)", rec.Code)
	}
	if rec := s.reqBody("PUT", "/api/v1/ext/com.a/kv/x", viewerSess, strings.NewReader(`{"value":"y"}`)); rec.Code != 403 {
		t.Errorf("viewer session PUT = %d, want 403", rec.Code)
	}
}
