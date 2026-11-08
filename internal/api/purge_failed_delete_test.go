package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

// A manual purge that can't delete a file must keep its row, as retention does:
// without the row nothing can ever find that file again.
func TestPurgesKeepRowsOfFilesTheyCouldNotDelete(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(s *Server, w http.ResponseWriter)
	}{
		{"storage purge", func(s *Server, w http.ResponseWriter) {
			s.handlePurge(w, httptest.NewRequest(http.MethodPost, "/",
				strings.NewReader(`{"cameraId":"camX","from":1,"to":100000}`)))
		}},
		{"playback remove", func(s *Server, w http.ResponseWriter) {
			r := httptest.NewRequest(http.MethodDelete, "/?from=1&to=100000", nil)
			rc := chi.NewRouteContext()
			rc.URLParams.Add("id", "camX")
			ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rc)
			ctx = auth.WithClaims(ctx, &auth.SessionClaims{UserID: "u1", Role: store.RoleAdmin, Class: auth.ClassSession})
			s.handleDeleteRecordings(w, r.WithContext(ctx))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			st, err := store.Open(filepath.Join(dir, "purge.db"))
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			t.Cleanup(func() { _ = st.Close() })
			s := &Server{
				st:  st,
				cfg: &config.Config{DataDir: dir, RecordingsDir: filepath.Join(dir, "recordings")},
				log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			if err := st.CreateCamera(&store.Camera{ID: "camX", Name: "X"}); err != nil {
				t.Fatal(err)
			}
			good := filepath.Join(dir, "good.ts")
			if err := os.WriteFile(good, []byte("xx"), 0o644); err != nil {
				t.Fatal(err)
			}
			// A directory with something in it can't be removed, even by root.
			bad := filepath.Join(dir, "bad.ts")
			if err := os.MkdirAll(filepath.Join(bad, "x"), 0o755); err != nil {
				t.Fatal(err)
			}
			for i, p := range []string{good, bad} {
				if err := st.InsertSegment(&store.Segment{ID: filepath.Base(p), CameraID: "camX", Path: p,
					StartTS: int64(1000 + i), EndTS: int64(2000 + i), DurationMS: 1000, SizeBytes: 2}); err != nil {
					t.Fatal(err)
				}
			}

			w := httptest.NewRecorder()
			c.run(s, w)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}

			var got struct{ DeletedSegments, FailedSegments int }
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.DeletedSegments != 1 || got.FailedSegments != 1 {
				t.Errorf("deleted %d, failed %d, want 1 of each", got.DeletedSegments, got.FailedSegments)
			}
			if _, err := st.SegmentByID("bad.ts"); err != nil {
				t.Error("the row went while its file stayed")
			}
			if _, err := st.SegmentByID("good.ts"); err == nil {
				t.Error("the deletable segment's row stayed")
			}
		})
	}
}
