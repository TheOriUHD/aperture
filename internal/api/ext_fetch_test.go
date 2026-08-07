package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestExtFetch covers host-mediated egress: it reaches ONLY declared hosts, injects
// {{secret:key}} server-side (the plaintext never travels from the client), and
// refuses an undeclared host, an unknown secret, and an extension with no network.
func TestExtFetch(t *testing.T) {
	// The "declared host": an echo server that reports the Authorization it received.
	var gotAuth, gotBody string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		gotBody = string(b)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()

	s := newTestServer(t)
	admin := &store.User{ID: "admin", Email: "a@x", Name: "Admin", Role: store.RoleAdmin}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.SetSetting("ext.enabled", "1"))
	adminSess := cred{apply: cookieCred(sessionCookieFor(t, s.am, admin))}

	manifest := `{"manifest":1,"id":"com.net","name":"N","version":"1.0.0",
	  "ui":{"panels":[{"slot":"camera-detail","entry":"ui/p.html"}]},
	  "permissions":{"network":["` + target.URL + `"]},
	  "settings":[{"key":"ha_token","label":"Token","type":"secret"}]}`
	mustDo(t, s.st.CreateExtension(&store.Extension{ID: "com.net", Name: "N", Version: "1.0.0", Source: "sideload", Enabled: true, ManifestJSON: manifest, InstalledBy: admin.ID}))
	mustDo(t, s.st.ExtKVSet("com.net", "ha_token", "TOKEN123"))

	// Declared host + secret injection: the echo server sees the real token.
	body := `{"url":"` + target.URL + `/api/x","method":"POST","headers":{"Authorization":"Bearer {{secret:ha_token}}"},"body":"{\"e\":1}"}`
	rec := s.reqBody("POST", "/api/v1/ext/com.net/fetch", adminSess, strings.NewReader(body))
	if rec.Code != 200 {
		t.Fatalf("declared-host fetch = %d (%s)", rec.Code, rec.Body)
	}
	if gotAuth != "Bearer TOKEN123" {
		t.Fatalf("secret injection: upstream saw %q, want Bearer TOKEN123", gotAuth)
	}
	if gotBody != `{"e":1}` {
		t.Fatalf("body passthrough: upstream saw %q", gotBody)
	}
	var resp struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	if resp.Status != 200 || !strings.Contains(resp.Body, "ok") {
		t.Fatalf("relayed response wrong: %+v", resp)
	}
	// The response must not echo the injected secret back to the client.
	if strings.Contains(rec.Body.String(), "TOKEN123") {
		t.Fatalf("secret leaked back to client: %s", rec.Body)
	}

	// An UNDECLARED host is refused.
	if rec := s.reqBody("POST", "/api/v1/ext/com.net/fetch", adminSess,
		strings.NewReader(`{"url":"https://evil.example/x","method":"GET"}`)); rec.Code != 403 {
		t.Errorf("undeclared host = %d, want 403", rec.Code)
	}
	// Referencing a secret the manifest never declared is refused.
	if rec := s.reqBody("POST", "/api/v1/ext/com.net/fetch", adminSess,
		strings.NewReader(`{"url":"`+target.URL+`/x","headers":{"X":"{{secret:not_a_secret}}"}}`)); rec.Code != 400 {
		t.Errorf("unknown secret ref = %d, want 400", rec.Code)
	}

	// An extension with no network permission cannot use egress at all.
	noNet := `{"manifest":1,"id":"com.nonet","name":"NN","version":"1.0.0","ui":{"panels":[{"slot":"camera-detail","entry":"ui/p.html"}]}}`
	mustDo(t, s.st.CreateExtension(&store.Extension{ID: "com.nonet", Name: "NN", Version: "1.0.0", Source: "sideload", Enabled: true, ManifestJSON: noNet, InstalledBy: admin.ID}))
	if rec := s.reqBody("POST", "/api/v1/ext/com.nonet/fetch", adminSess,
		strings.NewReader(`{"url":"`+target.URL+`/x"}`)); rec.Code != 403 {
		t.Errorf("no-network egress = %d, want 403", rec.Code)
	}
}

// TestExtFetchSecretsAreNamespaceScoped is the security assertion for {{secret:key}}:
// the substitution resolves ONLY against the calling extension's own declared secrets
// and its own ext_kv namespace. An extension can neither name another extension's
// secret key nor, when the two share a key name, read the other's value — the key is
// always read from `ExtKVGet(routeExtID, key)`, keyed by the extension in the route.
func TestExtFetchSecretsAreNamespaceScoped(t *testing.T) {
	var gotAuth string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer target.Close()

	s := newTestServer(t)
	admin := &store.User{ID: "admin", Email: "a@x", Name: "Admin", Role: store.RoleAdmin}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.SetSetting("ext.enabled", "1"))
	adminSess := cred{apply: cookieCred(sessionCookieFor(t, s.am, admin))}

	// Two extensions, each granted the SAME host, each with its own secret plus a
	// secret whose KEY NAME collides ("shared") but whose value differs.
	manifest := func(id, ownKey string) string {
		return `{"manifest":1,"id":"` + id + `","name":"` + id + `","version":"1.0.0",
		  "ui":{"panels":[{"slot":"camera-detail","entry":"ui/p.html"}]},
		  "permissions":{"network":["` + target.URL + `"]},
		  "settings":[{"key":"` + ownKey + `","label":"K","type":"secret"},{"key":"shared","label":"S","type":"secret"}]}`
	}
	mustDo(t, s.st.CreateExtension(&store.Extension{ID: "com.a", Name: "A", Version: "1.0.0", Source: "sideload", Enabled: true, ManifestJSON: manifest("com.a", "key_a"), InstalledBy: admin.ID}))
	mustDo(t, s.st.CreateExtension(&store.Extension{ID: "com.b", Name: "B", Version: "1.0.0", Source: "sideload", Enabled: true, ManifestJSON: manifest("com.b", "key_b"), InstalledBy: admin.ID}))
	mustDo(t, s.st.ExtKVSet("com.a", "key_a", "A-ONLY"))
	mustDo(t, s.st.ExtKVSet("com.a", "shared", "A-SHARED"))
	mustDo(t, s.st.ExtKVSet("com.b", "key_b", "B-ONLY"))
	mustDo(t, s.st.ExtKVSet("com.b", "shared", "B-SHARED"))

	do := func(routeID, ref string) *httptest.ResponseRecorder {
		gotAuth = ""
		body := `{"url":"` + target.URL + `/x","method":"GET","headers":{"Authorization":"Bearer {{secret:` + ref + `}}"}}`
		return s.reqBody("POST", "/api/v1/ext/"+routeID+"/fetch", adminSess, strings.NewReader(body))
	}

	// A referencing B's OWN secret key → refused (not a declared secret of A), and B's
	// value is never sent upstream.
	if rec := do("com.a", "key_b"); rec.Code != 400 {
		t.Fatalf("A referencing B's key = %d, want 400 (refused)", rec.Code)
	}
	if gotAuth != "" {
		t.Fatalf("cross-extension secret must never reach upstream, saw %q", gotAuth)
	}

	// Positive control: A referencing its OWN secret works.
	if rec := do("com.a", "key_a"); rec.Code != 200 || gotAuth != "Bearer A-ONLY" {
		t.Fatalf("A's own secret = %d auth=%q, want 200 Bearer A-ONLY", rec.Code, gotAuth)
	}

	// Colliding key name: A's {{secret:shared}} reads A's namespace, never B's.
	if rec := do("com.a", "shared"); rec.Code != 200 || gotAuth != "Bearer A-SHARED" {
		t.Fatalf("A's shared secret = %d auth=%q, want 200 Bearer A-SHARED (A's namespace)", rec.Code, gotAuth)
	}
	if rec := do("com.b", "shared"); rec.Code != 200 || gotAuth != "Bearer B-SHARED" {
		t.Fatalf("B's shared secret = %d auth=%q, want 200 Bearer B-SHARED (B's namespace)", rec.Code, gotAuth)
	}
}
