package ext

import (
	"os"
	"path/filepath"
	"testing"
)

// TestShippedExampleResolves guards the canonical example authors copy: it must
// parse, validate, and be coherent under the current model (opt-in cameras, secret
// settings implying storage, a declared network host). If a model change breaks the
// example, this fails here rather than at install for a user.
func TestShippedExampleResolves(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "examples", "ha-room-lights", "manifest.json"))
	if err != nil {
		t.Skipf("example manifest not found: %v", err)
	}
	m, err := Parse(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d, err := m.Validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if CameraDataWithoutRequest(d) {
		t.Fatalf("the example must be install-coherent (declares cameras for its camera-data scope)")
	}
	if !d.RequestsCameras || !d.RequestsAll {
		t.Fatalf("the example requests all cameras: got requests=%v all=%v", d.RequestsCameras, d.RequestsAll)
	}
	if !d.HasStorage {
		t.Fatalf("declaring settings must imply storage")
	}
	wantSecret := false
	for _, k := range d.SecretKeys {
		if k == "ha_token" {
			wantSecret = true
		}
	}
	if !wantSecret {
		t.Fatalf("the HA token must be a secret setting; secret keys = %v", d.SecretKeys)
	}
	if len(d.NetHosts) != 1 || d.NetHosts[0] != "homeassistant.local:8123" {
		t.Fatalf("network host = %v, want [homeassistant.local:8123]", d.NetHosts)
	}
}

// TestMotionWebhookExampleResolves guards the canonical BACKEND example: a headless
// extension that reacts to the feed on granted cameras and makes one outbound call. It
// must be a coherent backend (events:read WITH an explicit camera request), imply
// storage from its settings, and declare its one network host.
func TestMotionWebhookExampleResolves(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "examples", "motion-webhook", "manifest.json"))
	if err != nil {
		t.Skipf("example manifest not found: %v", err)
	}
	m, err := Parse(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d, err := m.Validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !d.HasBackend {
		t.Fatalf("the motion-webhook example is a backend extension")
	}
	if CameraDataWithoutRequest(d) || !d.RequestsCameras {
		t.Fatalf("reads events, so it must request cameras and be install-coherent")
	}
	hasEvents := false
	for _, s := range d.Scopes {
		if s == "events:read" {
			hasEvents = true
		}
	}
	if !hasEvents {
		t.Fatalf("must hold events:read to react to the feed; scopes = %v", d.Scopes)
	}
	if !d.HasStorage {
		t.Fatalf("declaring settings must imply storage")
	}
	if len(d.NetHosts) != 1 || d.NetHosts[0] != "hooks.example.com" {
		t.Fatalf("network host = %v, want [hooks.example.com]", d.NetHosts)
	}
}
