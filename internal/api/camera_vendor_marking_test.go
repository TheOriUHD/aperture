package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// The vendor marking exists because the old behaviour inferred the vendor from a
// live ONVIF probe. That inference evaporated whenever the probe could not run —
// camera offline, ONVIF switched off, credentials edited — so vendor controls the
// operator had been using vanished after an unrelated settings change, with no
// explanation. These tests pin the two properties that fix that: the marking is
// stored, and an edit that does not mention it leaves it alone.
func TestCameraVendorMarkingPersists(t *testing.T) {
	s := newTestServer(t)
	if err := s.st.CreateCamera(&store.Camera{
		ID: "cam1", Name: "Zimmer", RTSPMainURL: "rtsp://u:p@10.0.0.5:554/x", RecordMode: "continuous",
	}); err != nil {
		t.Fatal(err)
	}

	put := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/cameras/cam1", strings.NewReader(body))
		r.Header.Set("content-type", "application/json")
		r.Header.Set("Authorization", "Bearer "+testCLIToken)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, r)
		return w
	}
	vendorNow := func(t *testing.T) string {
		t.Helper()
		c, err := s.st.GetCamera("cam1")
		if err != nil {
			t.Fatal(err)
		}
		return c.Vendor
	}

	if got := vendorNow(t); got != "" {
		t.Fatalf("a new camera should be unmarked, got %q", got)
	}

	if w := put(`{"vendor":"reolink"}`); w.Code != http.StatusOK {
		t.Fatalf("marking as reolink: HTTP %d — %s", w.Code, w.Body.String())
	}
	if got := vendorNow(t); got != store.VendorReolink {
		t.Fatalf("vendor = %q, want %q", got, store.VendorReolink)
	}

	// The regression that started all this: change something ELSE about the camera
	// and the marking must survive.
	if w := put(`{"name":"Zimmer PH","retentionDays":9}`); w.Code != http.StatusOK {
		t.Fatalf("unrelated edit: HTTP %d — %s", w.Code, w.Body.String())
	}
	if got := vendorNow(t); got != store.VendorReolink {
		t.Fatalf("an unrelated settings edit cleared the vendor marking (got %q)", got)
	}

	// It must also be readable back over the API, or the settings toggle cannot
	// render its own state.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/cameras/cam1", nil)
	r.Header.Set("Authorization", "Bearer "+testCLIToken)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, r)
	var view map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view["vendor"] != store.VendorReolink {
		t.Fatalf("camera view vendor = %v, want %q", view["vendor"], store.VendorReolink)
	}

	// And it must be clearable.
	if w := put(`{"vendor":""}`); w.Code != http.StatusOK {
		t.Fatalf("clearing: HTTP %d", w.Code)
	}
	if got := vendorNow(t); got != "" {
		t.Fatalf("vendor should be cleared, got %q", got)
	}
}

// A marking Aperture does not act on is stored as "" rather than verbatim: one that
// drives nothing but looks set would leave the operator wondering why the controls
// never appeared.
func TestValidVendorRejectsUnknown(t *testing.T) {
	cases := map[string]string{
		"reolink":   store.VendorReolink,
		"Reolink":   store.VendorReolink,
		" REOLINK ": store.VendorReolink,
		"":          "",
		"hikvision": "",
		"reolinkx":  "",
	}
	for in, want := range cases {
		if got := validVendor(in); got != want {
			t.Errorf("validVendor(%q) = %q, want %q", in, got, want)
		}
	}
}

// Reading vendor state talks to the CAMERA over the vendor's own HTTP API. A Reolink
// serves very few concurrent sessions, so repeated reads alongside the RTSP pull that
// recording depends on can wedge it — the camera keeps answering pings while its
// stream stops delivering. The read is therefore rate-limited server-side, so no
// client can reach the camera more often than vendorTTL regardless of how it behaves.
func TestVendorReadIsCachedAwayFromTheCamera(t *testing.T) {
	invalidateVendorRead("camC")

	if _, ok := cachedVendorRead("camC"); ok {
		t.Fatal("nothing stored yet, but a cached read was reported")
	}

	body := map[string]any{"vendor": "reolink", "supported": true}
	storeVendorRead("camC", body)

	got, ok := cachedVendorRead("camC")
	if !ok {
		t.Fatal("a fresh read should be served from cache, sparing the camera")
	}
	if got["vendor"] != "reolink" {
		t.Fatalf("cached body = %v", got)
	}

	// A write changes the camera, so the cached read must not outlive it.
	invalidateVendorRead("camC")
	if _, ok := cachedVendorRead("camC"); ok {
		t.Fatal("a write must drop the cached read, or the UI shows the old state")
	}

	// An entry older than the TTL is not served.
	vendorCacheMu.Lock()
	vendorCache["camC"] = vendorSnapshot{at: time.Now().Add(-vendorTTL - time.Second), body: body}
	vendorCacheMu.Unlock()
	if _, ok := cachedVendorRead("camC"); ok {
		t.Fatal("a stale entry must expire rather than being served forever")
	}
}
