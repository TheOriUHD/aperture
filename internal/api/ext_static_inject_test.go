package api

import (
	"strings"
	"testing"
)

// A panel that omits <head>/<body> is legal HTML and the documented starter does
// exactly that. The SDK must never end up before the doctype: that puts the panel in
// quirks mode, and a quirks-mode document loses the transparent canvas the overlay
// slots rely on — it paints an opaque rectangle over the live video instead.
func TestInjectSDKKeepsDoctypeFirst(t *testing.T) {
	cases := []struct {
		name string
		html string
	}{
		{"no head or body", "<!doctype html>\n<meta charset=\"utf-8\">\n<style>body{}</style>\n<div>hi</div>"},
		{"uppercase doctype", "<!DOCTYPE HTML>\n<style>body{}</style>"},
		{"doctype with whitespace", "<!doctype html >\n<div>x</div>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := string(injectSDK(tc.html))
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(out)), "<!doctype") {
				t.Fatalf("doctype is no longer first; got: %.60q", out)
			}
			if !strings.Contains(out, "window.aperture") {
				t.Fatal("SDK was not injected at all")
			}
		})
	}
}

func TestInjectSDKPrefersHeadThenBody(t *testing.T) {
	out := string(injectSDK("<!doctype html><html><head><title>x</title></head><body>y</body></html>"))
	head := strings.Index(out, "</head>")
	sdk := strings.Index(out, "window.aperture")
	if sdk < 0 || sdk > head {
		t.Fatal("SDK should be injected inside <head> when one exists")
	}

	out = string(injectSDK("<!doctype html><body>y</body>"))
	if !strings.Contains(out, "window.aperture") {
		t.Fatal("SDK should be injected after <body> when there is no head")
	}
}

// A fragment with no doctype at all still has to get the SDK — just without any
// promise about quirks mode, which is the author's choice at that point.
func TestInjectSDKFragmentStillGetsSDK(t *testing.T) {
	if !strings.Contains(string(injectSDK("<div>only a fragment</div>")), "window.aperture") {
		t.Fatal("SDK missing from a fragment panel")
	}
}

// TestInjectSDKPinsColourScheme guards the rule that keeps an overlay panel
// transparent. Per CSS Color Adjust §2.4 a UA must paint an OPAQUE canvas when an
// iframe element's used color-scheme differs from the embedded document's, and
// color-scheme inherits — so a panel frame computes `dark` from the app's :root
// while a panel that declares nothing computes light. That mismatch, not the panel's
// own background, is what painted a white rectangle over the live video. The pin has
// to reach EVERY panel, including third-party bundles that never ask for it, through
// whichever insertion branch injectSDK takes.
func TestInjectSDKPinsColourScheme(t *testing.T) {
	docs := map[string]string{
		"full document":  "<!doctype html><html><head><style>body{margin:0}</style></head><body>hi</body></html>",
		"body only":      "<!doctype html><body>hi</body>",
		"doctype only":   "<!doctype html><p>hi</p>",
		"bare fragment":  "<p>hi</p>",
		"uppercase HEAD": "<!DOCTYPE HTML><HTML><HEAD></HEAD><BODY>hi</BODY></HTML>",
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			got := string(injectSDK(doc))
			if !strings.Contains(got, "color-scheme:dark") {
				t.Fatalf("panel document was served without the colour-scheme pin:\n%s", got)
			}
			// It must also still carry the SDK, and the doctype must stay first —
			// a quirks-mode panel loses other guarantees the slots depend on.
			if !strings.Contains(got, "window.aperture") {
				t.Errorf("SDK bootstrap missing")
			}
			if i := strings.Index(strings.ToLower(doc), "<!doctype"); i >= 0 {
				if !strings.HasPrefix(strings.ToLower(got), "<!doctype") {
					t.Errorf("doctype no longer first; document would parse in quirks mode:\n%s", got)
				}
			}
		})
	}
}

// The SDK's automatic sizing must measure the BODY. scrollHeight on the root element
// is floored by the frame's own viewport, so once a frame has been given a height it
// could never report 0 again — an empty overlay would latch open forever.
func TestSDKMeasuresBodyNotRootElement(t *testing.T) {
	if !strings.Contains(extSDKBootstrap, "document.body") {
		t.Fatal("SDK auto-resize must measure document.body, or an empty frame latches open")
	}
	if strings.Contains(extSDKBootstrap, "resize(document.documentElement.scrollHeight)") {
		t.Error("SDK still reports the viewport-floored root scrollHeight")
	}
}
