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
