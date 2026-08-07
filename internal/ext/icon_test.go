package ext

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestValidateIcon(t *testing.T) {
	if err := ValidateIcon(pngBytes(t, 32, 32)); err != nil {
		t.Errorf("a valid 32x32 PNG was rejected: %v", err)
	}

	// THE security case: a nav-rail icon is chosen by the host and rendered in host
	// chrome. An SVG carrying a <script> tag, shipped under an icon name, must be
	// rejected at install — it is not a PNG, so it can never be served as a pinned
	// icon and its script can never execute in the user's session context.
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24">` +
		`<script>fetch('/api/v1/system/settings')</script></svg>`)
	if err := ValidateIcon(svg); err == nil {
		t.Error("an SVG containing <script> was accepted as an icon — it MUST be rejected")
	}

	for name, b := range map[string][]byte{
		"empty":       nil,
		"shell script": []byte("#!/bin/sh\necho hi\n"),
		"html":        []byte("<!doctype html><script>alert(1)</script>"),
		"jpeg-magic":  {0xFF, 0xD8, 0xFF, 0xE0}, // JPEG SOI — a real image, but not PNG
	} {
		if err := ValidateIcon(b); err == nil {
			t.Errorf("%s was accepted as an icon; want rejection", name)
		}
	}

	if err := ValidateIcon(pngBytes(t, 512, 512)); err == nil {
		t.Error("a 512x512 PNG was accepted; it exceeds the max icon dimension")
	}
}

func TestManifestIconPathValidation(t *testing.T) {
	base := func(icon string) *Manifest {
		return &Manifest{
			Manifest: 1, ID: "com.x.y", Name: "X", Version: "1.0.0",
			UI:   &UISpec{Panels: []Panel{{Slot: "nav-panel", Entry: "ui/p.html", Title: "P"}}},
			Icon: icon,
		}
	}
	if _, err := base("ui/icon.png").Validate(); err != nil {
		t.Errorf("a valid icon path was rejected: %v", err)
	}
	for _, bad := range []string{"icon.png", "../icon.png", "ui/icon.svg", "/etc/icon.png"} {
		if _, err := base(bad).Validate(); err == nil {
			t.Errorf("bad icon path %q was accepted", bad)
		}
	}
	// A declared nav-panel makes the extension pin-eligible.
	d, err := base("ui/icon.png").Validate()
	if err != nil || !d.HasNavPanel {
		t.Errorf("nav-panel not detected for pin eligibility: %v", err)
	}
}
