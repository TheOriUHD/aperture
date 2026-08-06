package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestEventsAllCameraACLInSQL covers T2.7: the cross-camera events feed filters
// by the caller's camera ACL in SQL, not by over-fetching and dropping rows.
// The regression it pins: when the newest events all belong to a camera the
// viewer cannot see, the old 4x over-fetch returned FEWER (often zero) of the
// viewer's own events; the SQL filter returns up to `limit` of them.
func TestEventsAllCameraACLInSQL(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	viewer := &store.User{ID: "v1", Email: "v@t", Name: "V", Role: "viewer"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateUser(viewer))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "Own"}))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam2", Name: "Other"}))
	mustDo(t, s.st.SetUserCameras(viewer.ID, []string{"cam1"}))

	const T = int64(1_700_000_000_000)
	// 10 older events on cam1, then 25 NEWER events on cam2. With the old
	// limit*4 over-fetch and a small limit, the newest fetched rows are all cam2,
	// so a cam1-only viewer would see none of its own events.
	for i := 0; i < 10; i++ {
		mustDo(t, s.st.InsertEvent(&store.Event{CameraID: "cam1", StartTS: T + int64(i)}))
	}
	for i := 0; i < 25; i++ {
		mustDo(t, s.st.InsertEvent(&store.Event{CameraID: "cam2", StartTS: T + 1_000 + int64(i)}))
	}

	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}
	viewerC := cred{"viewer", cookieCred(sessionCookieFor(t, s.am, viewer))}
	rng := fmt.Sprintf("from=%d&to=%d", T-1, T+10_000)

	events := func(c cred, limit int) []map[string]any {
		t.Helper()
		rec := s.reqAs("GET", fmt.Sprintf("/api/v1/events?%s&limit=%d", rng, limit), c)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /events: got %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		var out []map[string]any
		mustDo(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out
	}

	// Viewer, small limit: gets its OWN newest events despite 25 newer cam2 rows.
	got := events(viewerC, 5)
	if len(got) != 5 {
		t.Fatalf("viewer limit=5: got %d events, want 5 (regression: over-fetch under-filled)", len(got))
	}
	for _, e := range got {
		if e["cameraId"] != "cam1" {
			t.Fatalf("viewer saw cameraId=%v, want only cam1", e["cameraId"])
		}
	}

	// Viewer, large limit: exactly its 10 cam1 events, never a cam2 row.
	if got := events(viewerC, 100); len(got) != 10 {
		t.Fatalf("viewer limit=100: got %d, want all 10 cam1 events", len(got))
	}

	// Admin sees across cameras: the 5 newest are cam2.
	got = events(adminC, 5)
	if len(got) != 5 {
		t.Fatalf("admin limit=5: got %d, want 5", len(got))
	}
	for _, e := range got {
		if e["cameraId"] != "cam2" {
			t.Fatalf("admin newest should be cam2, got %v", e["cameraId"])
		}
	}
}

// TestEventsAllViewerNoCameras: a viewer with an empty ACL gets no rows (the
// empty-slice filter yields nothing rather than falling through to all cameras).
func TestEventsAllViewerNoCameras(t *testing.T) {
	s := newTestServer(t)
	viewer := &store.User{ID: "v1", Email: "v@t", Name: "V", Role: "viewer"}
	mustDo(t, s.st.CreateUser(viewer))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "Own"}))
	const T = int64(1_700_000_000_000)
	mustDo(t, s.st.InsertEvent(&store.Event{CameraID: "cam1", StartTS: T}))

	viewerC := cred{"viewer", cookieCred(sessionCookieFor(t, s.am, viewer))}
	rec := s.reqAs("GET", fmt.Sprintf("/api/v1/events?from=%d&to=%d", T-1, T+1), viewerC)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var out []map[string]any
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &out))
	if len(out) != 0 {
		t.Fatalf("viewer with no cameras got %d events, want 0", len(out))
	}
}
