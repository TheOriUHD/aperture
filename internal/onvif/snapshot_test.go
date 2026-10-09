package onvif

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

type fakeSnaps struct{ fail bool }

func (f fakeSnaps) SnapshotCached(_ context.Context, id string) ([]byte, error) {
	if f.fail {
		return nil, errors.New("go2rtc down")
	}
	return []byte("jpeg-of-" + id), nil
}

func newSnapshotTestServer(t *testing.T, snaps Snapshotter) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, c := range []*store.Camera{
		{ID: "on", Name: "On", RTSPMainURL: "rtsp://u:secret@10.0.0.5/x", Enabled: true},
		{ID: "off", Name: "Off", RTSPMainURL: "rtsp://u:secret@10.0.0.6/x", Enabled: false},
	} {
		if err := st.CreateCamera(c); err != nil {
			t.Fatal(err)
		}
	}
	return New(&config.Config{ONVIFAddr: ":10101"}, st, snaps, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// TestSnapshotServedByProviderNotGo2RTC pins that GetSnapshotUri no longer points
// clients at go2rtc's API port (bound to localhost, because that API hands out
// every camera's password), and that the provider's own snapshot route serves an
// enabled camera's frame and nothing else.
func TestSnapshotServedByProviderNotGo2RTC(t *testing.T) {
	s := newSnapshotTestServer(t, fakeSnaps{})

	u := s.snapshotURL("on")
	if strings.Contains(u, ":1984") || strings.Contains(u, "frame.jpeg") {
		t.Fatalf("snapshot link still points at go2rtc: %s", u)
	}
	if !strings.HasSuffix(u, ":10101/onvif/snapshot/on.jpg") {
		t.Fatalf("snapshot link = %s, want it on the ONVIF port", u)
	}

	h := s.routes()
	get := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}

	rec := get(http.MethodGet, "/onvif/snapshot/on.jpg")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" || rec.Body.String() != "jpeg-of-on" {
		t.Fatalf("enabled camera: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	for _, p := range []string{"/onvif/snapshot/off.jpg", "/onvif/snapshot/nope.jpg"} {
		if rec := get(http.MethodGet, p); rec.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", p, rec.Code)
		}
	}
	if rec := get(http.MethodPost, "/onvif/snapshot/on.jpg"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: got %d, want 405", rec.Code)
	}

	failing := newSnapshotTestServer(t, fakeSnaps{fail: true})
	rec = httptest.NewRecorder()
	failing.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/onvif/snapshot/on.jpg", nil))
	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("failed fetch: %d %q", rec.Code, rec.Body.String())
	}
}
