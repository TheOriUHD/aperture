package ext

import (
	"reflect"
	"strings"
	"testing"
)

const haLights = `{
  "manifest": 1,
  "id": "com.example.ha-room-lights",
  "name": "Home Assistant Room Lights",
  "version": "1.2.0",
  "author": "Jane Doe",
  "description": "Adds a camera-view panel and turns on the room's lights.",
  "minApiVersion": "v1",
  "permissions": {
    "data": { "cameras": "read", "events": "read" },
    "cameras": ["hallway", "front-door"],
    "network": ["https://homeassistant.local:8123"],
    "storage": true
  },
  "ui": { "panels": [ { "slot": "camera-detail", "entry": "ui/panel.html", "title": "Room Lights" } ] },
  "backend": { "entry": "backend/main.ts", "denoVersion": "2" },
  "update": { "url": "https://example.com/ha-room-lights/manifest.json" }
}`

func TestManifestHALightsResolves(t *testing.T) {
	m, err := Parse([]byte(haLights))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d, err := m.Validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !d.HasUI || !d.HasBackend {
		t.Fatalf("HasUI/HasBackend = %v/%v, want true/true", d.HasUI, d.HasBackend)
	}
	if want := []string{"cameras:read", "events:read"}; !reflect.DeepEqual(d.Scopes, want) {
		t.Fatalf("scopes = %v, want %v", d.Scopes, want)
	}
	if want := []string{"hallway", "front-door"}; !reflect.DeepEqual(d.CameraGrants, want) {
		t.Fatalf("cameraGrants = %v, want %v", d.CameraGrants, want)
	}
	if !d.RequestsCameras || d.RequestsAll {
		t.Fatalf("a specific camera list = RequestsCameras && !RequestsAll, got %v/%v", d.RequestsCameras, d.RequestsAll)
	}
	if d.CameraConsent == "" {
		t.Fatalf("a camera-requesting extension must carry a camera-consent line")
	}
	if want := []string{"homeassistant.local:8123"}; !reflect.DeepEqual(d.NetHosts, want) {
		t.Fatalf("netHosts = %v, want %v (scheme stripped, port kept)", d.NetHosts, want)
	}
	// The generic consent lines warn about outbound + storage + the hard "cannot".
	// Camera specifics live in CameraConsent + the picker, NOT these lines.
	joined := strings.Join(d.ConsentLines, "\n")
	for _, must := range []string{"homeassistant.local:8123", "private folder", "cannot read your recordings"} {
		if !strings.Contains(joined, must) {
			t.Errorf("consent missing %q in:\n%s", must, joined)
		}
	}
}

func TestManifestWriteImpliesRead(t *testing.T) {
	m, _ := Parse([]byte(`{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"data":{"detection":"write"}}}`))
	d, err := m.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"detection:read", "detection:write"}; !reflect.DeepEqual(d.Scopes, want) {
		t.Fatalf("write should imply read: %v, want %v", d.Scopes, want)
	}
}

func TestManifestRejections(t *testing.T) {
	cases := map[string]string{
		"unknown top-level field (strict)": `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"bogus":1}`,
		"wrong manifest version":           `{"manifest":9,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"}}`,
		"bad id (not reverse-DNS)":         `{"manifest":1,"id":"notdns","name":"N","version":"1.0.0","backend":{"entry":"m.ts"}}`,
		"bad version":                      `{"manifest":1,"id":"a.b","name":"N","version":"1.x","backend":{"entry":"m.ts"}}`,
		"no ui and no backend":             `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0"}`,
		"unknown data domain":              `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"data":{"nope":"read"}}}`,
		"un-grantable value (coverage write)": `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"data":{"coverage":"write"}}}`,
		"bad alarm value":                  `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"alarm":"disarm"}}`,
		"bad network url":                  `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"network":["not a url"]}}`,
		"bad camera request (string)":      `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"cameras":"some"}}`,
		"empty camera id":                  `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"cameras":["ok",""]}}`,
		"unknown setting type":             `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"settings":[{"key":"k","label":"L","type":"colour"}]}`,
		"select without options":           `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"settings":[{"key":"k","label":"L","type":"select"}]}`,
		"duplicate setting key":            `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"settings":[{"key":"k","label":"A","type":"text"},{"key":"k","label":"B","type":"text"}]}`,
		"bad setting key charset":          `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"settings":[{"key":"Bad Key","label":"L","type":"text"}]}`,
		"unknown ui slot":                  `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","ui":{"panels":[{"slot":"nope","entry":"ui/x.html"}]}}`,
		"entry path traversal":             `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"../escape.ts"}}`,
		"entry absolute path":              `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"/etc/passwd"}}`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			m, err := Parse([]byte(src))
			if err != nil {
				return // rejected at parse — acceptable (e.g. strict/version)
			}
			if _, err := m.Validate(); err == nil {
				t.Fatalf("expected rejection, got none")
			}
		})
	}
}

func TestCameraModelExplicit(t *testing.T) {
	// "all" — an explicit request for every camera, incl. future ones.
	m, _ := Parse([]byte(`{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"data":{"events":"read"},"cameras":"all"}}`))
	d, err := m.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if !d.RequestsCameras || !d.RequestsAll || len(d.CameraGrants) != 0 {
		t.Fatalf(`"all" => RequestsCameras && RequestsAll && no grant list; got %v/%v/%v`, d.RequestsCameras, d.RequestsAll, d.CameraGrants)
	}

	// A specific list is a ceiling, not "all".
	m, _ = Parse([]byte(`{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"data":{"events":"read"},"cameras":["c1"]}}`))
	d, _ = m.Validate()
	if !d.RequestsCameras || d.RequestsAll || !reflect.DeepEqual(d.CameraGrants, []string{"c1"}) {
		t.Fatalf("specific list wrong: %v/%v/%v", d.RequestsCameras, d.RequestsAll, d.CameraGrants)
	}

	// Absent = NO camera access (a pure non-camera extension).
	m, _ = Parse([]byte(`{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"system":"read"}}`))
	d, _ = m.Validate()
	if d.RequestsCameras || d.CameraConsent != "" {
		t.Fatalf("absent cameras must not request cameras; got %v / %q", d.RequestsCameras, d.CameraConsent)
	}
}

func TestCameraDataWithoutRequestGate(t *testing.T) {
	// A fresh bundle reading camera data (events) without a cameras request is
	// incoherent under the opt-in model — the install gate must catch it.
	m, _ := Parse([]byte(`{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"data":{"events":"read"}}}`))
	d, _ := m.Validate()
	if !CameraDataWithoutRequest(d) {
		t.Fatalf("events:read without a cameras request should be flagged for install")
	}
	// The SAME manifest still Validates without error (lenient) — so an extension
	// installed before this model is never retro-broken on the spawn path.
	if _, err := m.Validate(); err != nil {
		t.Fatalf("Validate must stay lenient for legacy manifests, got %v", err)
	}
	// Declaring the cameras permission resolves the incoherence.
	m2, _ := Parse([]byte(`{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"data":{"events":"read"},"cameras":"all"}}`))
	d2, _ := m2.Validate()
	if CameraDataWithoutRequest(d2) {
		t.Fatalf("events:read WITH cameras:all is coherent")
	}
	// A non-camera extension is never flagged.
	m3, _ := Parse([]byte(`{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"system":"read"}}`))
	d3, _ := m3.Validate()
	if CameraDataWithoutRequest(d3) {
		t.Fatalf("a non-camera extension must never be flagged")
	}
}

func TestSettingsImplyStorageAndSecrets(t *testing.T) {
	src := `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},
	  "settings":[
	    {"key":"url","label":"URL","type":"text","required":true},
	    {"key":"token","label":"Token","type":"secret","required":true},
	    {"key":"mode","label":"Mode","type":"select","options":[{"value":"a","label":"A"}]}
	  ]}`
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if !d.HasStorage {
		t.Fatalf("declaring settings must imply storage")
	}
	if len(d.Settings) != 3 {
		t.Fatalf("settings length = %d, want 3", len(d.Settings))
	}
	if !reflect.DeepEqual(d.SecretKeys, []string{"token"}) {
		t.Fatalf("secret keys = %v, want [token]", d.SecretKeys)
	}
	// The storage consent line reflects that secrets may be stored.
	if !strings.Contains(strings.Join(d.ConsentLines, "\n"), "secrets you enter") {
		t.Fatalf("settings should produce a secret-aware storage consent line: %v", d.ConsentLines)
	}
}

func TestNetworkFromSetting(t *testing.T) {
	src := `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},
	  "permissions":{"network":["{{setting:ha_url}}","https://static.example:9000"]},
	  "settings":[{"key":"ha_url","label":"URL","type":"url","required":true}]}`
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.NetSettings, []string{"ha_url"}) {
		t.Fatalf("netSettings = %v, want [ha_url]", d.NetSettings)
	}
	if !reflect.DeepEqual(d.NetHosts, []string{"static.example:9000"}) {
		t.Fatalf("netHosts = %v, want [static.example:9000] (static entries still work)", d.NetHosts)
	}
	// Consent names the CHOICE, not a host the user never picked.
	joined := strings.Join(d.ConsentLines, "\n")
	if !strings.Contains(joined, "a server you configure in its settings") {
		t.Fatalf("consent should describe the configurable host: %v", d.ConsentLines)
	}

	// A ref to a NON-url setting (or a missing one) is rejected.
	for name, s := range map[string]string{
		"ref to non-url setting": `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"network":["{{setting:x}}"]},"settings":[{"key":"x","label":"X","type":"text"}]}`,
		"ref to missing setting": `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"network":["{{setting:nope}}"]}}`,
	} {
		mm, _ := Parse([]byte(s))
		if _, err := mm.Validate(); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestSettingsGroupsAndCamera(t *testing.T) {
	src := `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},
	  "permissions":{"cameras":"all"},
	  "settings":[
	    {"key":"mapping","label":"Camera → light","type":"group","fields":[
	      {"key":"camera","label":"Camera","type":"camera","required":true},
	      {"key":"entity","label":"Entity","type":"text","required":true}
	    ]}
	  ]}`
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.Validate()
	if err != nil {
		t.Fatalf("a group of camera+text must validate: %v", err)
	}
	if len(d.Settings) != 1 || d.Settings[0].Type != "group" || len(d.Settings[0].Fields) != 2 {
		t.Fatalf("group schema not carried through: %+v", d.Settings)
	}

	// Bad groups: no fields, a nested group, a secret inside a group.
	for name, s := range map[string]string{
		"group without fields": `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"settings":[{"key":"g","label":"G","type":"group"}]}`,
		"nested group":         `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"settings":[{"key":"g","label":"G","type":"group","fields":[{"key":"h","label":"H","type":"group","fields":[{"key":"x","label":"X","type":"text"}]}]}]}`,
		"secret inside group":  `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"settings":[{"key":"g","label":"G","type":"group","fields":[{"key":"s","label":"S","type":"secret"}]}]}`,
	} {
		mm, _ := Parse([]byte(s))
		if _, err := mm.Validate(); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestCheckNoRemoteImports(t *testing.T) {
	bad := "import { x } from \"https://evil.example/mod.ts\";\nimport { aperture } from \"@aperture/sdk\";"
	got := CheckNoRemoteImports(bad)
	if len(got) != 1 || got[0] != "https://evil.example/mod.ts" {
		t.Fatalf("remote import detection = %v, want [https://evil.example/mod.ts]", got)
	}
	if extra := CheckNoRemoteImports(`import { aperture } from "@aperture/sdk";\nconst d = await import("./local.ts");`); len(extra) != 0 {
		t.Fatalf("bare + relative imports should be clean, got %v", extra)
	}
	if fileScheme := CheckNoRemoteImports(`import x from "file:///etc/passwd";`); len(fileScheme) != 1 {
		t.Fatalf("file: scheme should be rejected, got %v", fileScheme)
	}
}
