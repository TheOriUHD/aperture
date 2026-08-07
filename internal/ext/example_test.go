package ext

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
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
	// The network host now comes from the user's ha_url setting, not a hard-coded host.
	if len(d.NetHosts) != 0 || len(d.NetSettings) != 1 || d.NetSettings[0] != "ha_url" {
		t.Fatalf("network should resolve from the ha_url setting; hosts=%v settings=%v", d.NetHosts, d.NetSettings)
	}
	// It carries the camera→light mapping as a group of camera + entity.
	var group *SettingField
	for i := range d.Settings {
		if d.Settings[i].Type == "group" {
			group = &d.Settings[i]
		}
	}
	if group == nil || len(group.Fields) != 2 {
		t.Fatalf("the example must declare a camera→light group of 2 fields; settings=%+v", d.Settings)
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

// TestExampleBundlesAreFresh fails if any shipped examples/dist/<name>.zip drifts
// from the example source it was built from — a stale bundle fails the coherence gate
// at install and is a bad first experience for anyone following the docs, and nothing
// else caught it. Run `make examples` to rebuild.
//
// It compares CONTENTS, not mtimes: a git checkout stamps every file with the same
// time, so an mtime rule would both false-pass (all equal) and false-fail arbitrarily.
// A zip whose files byte-match the source is fresh by definition; anything else is not.
func TestExampleBundlesAreFresh(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("examples dir not found: %v", err)
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "dist" {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
			continue // not an example bundle
		}
		checked++
		zipFiles, err := readZipFiles(filepath.Join(root, "dist", e.Name()+".zip"))
		if err != nil {
			t.Errorf("%s: no distributable bundle (%v) — run `make examples`", e.Name(), err)
			continue
		}
		srcFiles, err := readDirFiles(dir)
		if err != nil {
			t.Fatalf("%s: reading source: %v", e.Name(), err)
		}
		for name, want := range srcFiles {
			got, ok := zipFiles[name]
			if !ok {
				t.Errorf("%s.zip is STALE: missing %q — run `make examples`", e.Name(), name)
			} else if !bytes.Equal(got, want) {
				t.Errorf("%s.zip is STALE: %q differs from source — run `make examples`", e.Name(), name)
			}
		}
		for name := range zipFiles {
			if _, ok := srcFiles[name]; !ok {
				t.Errorf("%s.zip is STALE: has %q not in source — run `make examples`", e.Name(), name)
			}
		}
	}
	if checked == 0 {
		t.Skip("no example bundles to check")
	}
}

func readZipFiles(path string) (map[string][]byte, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	out := map[string][]byte{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		out[f.Name] = b
	}
	return out, nil
}

func readDirFiles(dir string) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = b
		return nil
	})
	return out, err
}
