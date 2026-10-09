package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

const settingsManifest = `{"manifest":1,"id":"com.set","name":"S","version":"1.0.0","backend":{"entry":"main.ts"},
  "settings":[
    {"key":"ha_url","label":"URL","type":"text","required":true},
    {"key":"ha_token","label":"Token","type":"secret","required":true},
    {"key":"mode","label":"Mode","type":"select","options":[{"value":"a","label":"A"},{"value":"b","label":"B"}]}
  ]}`

// TestExtSettings covers the settings feature end to end: validated writes, the
// secret being write-only (never returned to a client, even an admin, on either the
// settings route OR raw KV), and the backend's token still reading the plaintext.
func TestExtSettings(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "admin", Email: "a@x", Name: "Admin", Role: store.RoleAdmin}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.SetSetting("ext.enabled", "1"))
	mustDo(t, s.st.CreateExtension(&store.Extension{ID: "com.set", Name: "S", Version: "1.0.0", Source: "sideload", Enabled: true, ManifestJSON: settingsManifest, InstalledBy: admin.ID}))
	adminSess := cred{apply: cookieCred(sessionCookieFor(t, s, admin))}

	// A valid write.
	if rec := s.reqBody("PUT", "/api/v1/ext/com.set/settings", adminSess,
		strings.NewReader(`{"values":{"ha_url":"https://ha.local","ha_token":"s3cr3t","mode":"a"}}`)); rec.Code != 200 {
		t.Fatalf("valid PUT = %d (%s)", rec.Code, rec.Body)
	}

	// GET returns non-secret values + secretSet, and NEVER the secret value.
	rec := s.reqAs("GET", "/api/v1/ext/com.set/settings", adminSess)
	if rec.Code != 200 {
		t.Fatalf("GET = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "s3cr3t") {
		t.Fatalf("secret value leaked in settings GET: %s", rec.Body)
	}
	var got struct {
		Values    map[string]string `json:"values"`
		SecretSet map[string]bool   `json:"secretSet"`
	}
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &got))
	if got.Values["ha_url"] != "https://ha.local" || got.Values["mode"] != "a" {
		t.Fatalf("non-secret values wrong: %+v", got.Values)
	}
	if !got.SecretSet["ha_token"] {
		t.Fatalf("secretSet should report ha_token as set")
	}

	// The secret is redacted on raw KV too — even for an admin session (write-only).
	if rec := s.reqAs("GET", "/api/v1/ext/com.set/kv/ha_token", adminSess); rec.Code != 404 {
		t.Fatalf("admin raw-KV GET of a secret = %d, want 404 (write-only)", rec.Code)
	}
	// But the extension's OWN backend token reads the plaintext (server-side).
	tok, err := s.st.CreateExtensionToken(admin.ID, "com.set", "com.set", nil, nil)
	mustDo(t, err)
	if rec := s.reqAs("GET", "/api/v1/ext/com.set/kv/ha_token", cred{apply: bearerCred(tok)}); rec.Code != 200 || !strings.Contains(rec.Body.String(), "s3cr3t") {
		t.Fatalf("backend token GET of its own secret = %d (%s), want 200 with value", rec.Code, rec.Body)
	}

	// An empty secret keeps the stored one; other fields still apply.
	if rec := s.reqBody("PUT", "/api/v1/ext/com.set/settings", adminSess,
		strings.NewReader(`{"values":{"ha_url":"https://ha2.local","ha_token":"","mode":"b"}}`)); rec.Code != 200 {
		t.Fatalf("PUT with empty secret = %d (%s)", rec.Code, rec.Body)
	}
	if v, _, _ := s.st.ExtKVGet("com.set", "ha_token"); v != "s3cr3t" {
		t.Fatalf("empty secret must keep the previous value, got %q", v)
	}
	if v, _, _ := s.st.ExtKVGet("com.set", "ha_url"); v != "https://ha2.local" {
		t.Fatalf("non-secret should update alongside an empty secret, got %q", v)
	}

	// Rejections: required-empty, unknown key, bad select option.
	for name, body := range map[string]string{
		"required non-secret empty": `{"values":{"ha_url":"","ha_token":"x"}}`,
		"unknown key":               `{"values":{"nope":"1"}}`,
		"bad select option":         `{"values":{"mode":"zzz"}}`,
	} {
		if rec := s.reqBody("PUT", "/api/v1/ext/com.set/settings", adminSess, strings.NewReader(body)); rec.Code != 400 {
			t.Errorf("%s: got %d, want 400 (%s)", name, rec.Code, rec.Body)
		}
	}

	// A required secret that was never set blocks the first save.
	mustDo(t, s.st.CreateExtension(&store.Extension{ID: "com.set2", Name: "S2", Version: "1.0.0", Source: "sideload", Enabled: true, ManifestJSON: strings.ReplaceAll(settingsManifest, "com.set", "com.set2"), InstalledBy: admin.ID}))
	if rec := s.reqBody("PUT", "/api/v1/ext/com.set2/settings", adminSess, strings.NewReader(`{"values":{"ha_url":"https://x","mode":"a"}}`)); rec.Code != 400 {
		t.Errorf("first save without the required secret = %d, want 400", rec.Code)
	}

	// Reads are authed but secret-safe: a viewer may read non-secret config for a
	// panel, and still never sees the secret.
	viewer := &store.User{ID: "viewer", Email: "v@x", Name: "V", Role: store.RoleViewer}
	mustDo(t, s.st.CreateUser(viewer))
	viewerSess := cred{apply: cookieCred(sessionCookieFor(t, s, viewer))}
	vr := s.reqAs("GET", "/api/v1/ext/com.set/settings", viewerSess)
	if vr.Code != 200 || strings.Contains(vr.Body.String(), "s3cr3t") {
		t.Fatalf("viewer settings GET = %d and must not leak the secret (%s)", vr.Code, vr.Body)
	}
}

const groupManifest = `{"manifest":1,"id":"com.grp","name":"G","version":"1.0.0","backend":{"entry":"m.ts"},
  "permissions":{"cameras":["cam1","cam2"]},
  "settings":[
    {"key":"ha_url","label":"URL","type":"url","required":true},
    {"key":"mapping","label":"Lights","type":"group","fields":[
      {"key":"camera","label":"Camera","type":"camera","required":true},
      {"key":"entity","label":"Entity","type":"text","required":true}
    ]}
  ]}`

// TestExtSettingsGroupsAndTypes covers the structured config the thin scalar schema
// couldn't express: a url (validated), and a repeatable group whose rows carry a
// camera picker + an entity — persisted as a JSON array and read back as one.
func TestExtSettingsGroupsAndTypes(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "admin", Email: "a@x", Name: "Admin", Role: store.RoleAdmin}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.SetSetting("ext.enabled", "1"))
	mustDo(t, s.st.CreateExtension(&store.Extension{ID: "com.grp", Name: "G", Version: "1.0.0", Source: "sideload", Enabled: true, ManifestJSON: groupManifest, CameraGrants: []string{"cam1", "cam2"}, InstalledBy: admin.ID}))
	adminSess := cred{apply: cookieCred(sessionCookieFor(t, s, admin))}

	// Save a url + a two-row camera→entity mapping.
	body := `{"values":{"ha_url":"https://ha.local:8123","mapping":[{"camera":"cam1","entity":"light.a"},{"camera":"cam2","entity":"light.b"}]}}`
	if rec := s.reqBody("PUT", "/api/v1/ext/com.grp/settings", adminSess, strings.NewReader(body)); rec.Code != 200 {
		t.Fatalf("group PUT = %d (%s)", rec.Code, rec.Body)
	}
	// Stored as a JSON array; read back as structured rows.
	rec := s.reqAs("GET", "/api/v1/ext/com.grp/settings", adminSess)
	var got struct {
		Values struct {
			HaURL   string              `json:"ha_url"`
			Mapping []map[string]string `json:"mapping"`
		} `json:"values"`
	}
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &got))
	if got.Values.HaURL != "https://ha.local:8123" {
		t.Fatalf("url value wrong: %q", got.Values.HaURL)
	}
	if len(got.Values.Mapping) != 2 || got.Values.Mapping[0]["camera"] != "cam1" || got.Values.Mapping[1]["entity"] != "light.b" {
		t.Fatalf("group not persisted as rows: %+v", got.Values.Mapping)
	}
	// The backend reads the same group back as its stored JSON (via its token).
	if v, _, _ := s.st.ExtKVGet("com.grp", "mapping"); !strings.Contains(v, `"entity":"light.a"`) {
		t.Fatalf("group not stored as JSON in ext_kv: %q", v)
	}

	// Rejections: a malformed URL, a camera outside the grant, a required subfield empty.
	for name, b := range map[string]string{
		"bad url":            `{"values":{"ha_url":"not a url","mapping":[]}}`,
		"camera not granted": `{"values":{"ha_url":"https://x.y","mapping":[{"camera":"cam9","entity":"light.z"}]}}`,
		"missing entity":     `{"values":{"ha_url":"https://x.y","mapping":[{"camera":"cam1","entity":""}]}}`,
	} {
		if rec := s.reqBody("PUT", "/api/v1/ext/com.grp/settings", adminSess, strings.NewReader(b)); rec.Code != 400 {
			t.Errorf("%s: got %d, want 400 (%s)", name, rec.Code, rec.Body)
		}
	}
}
