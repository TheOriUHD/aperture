package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestEventEndTsMsOngoing covers bug 4.6: an ongoing event serializes
// endTsMs=null (while legacy endTs stays populated); a finalized event has a
// real endTsMs.
func TestEventEndTsMsOngoing(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	const T = int64(1_700_000_000_000)
	ongoing := &store.Event{CameraID: "cam1", StartTS: T} // EndTS 0 = ongoing
	mustDo(t, s.st.InsertEvent(ongoing))
	done := &store.Event{CameraID: "cam1", StartTS: T + 1}
	mustDo(t, s.st.InsertEvent(done))
	mustDo(t, s.st.FinalizeEvent(done.ID, T+5000, 0.9, nil))

	get := func(id string) map[string]any {
		rec := s.reqAs("GET", "/api/v1/events/"+id, adminC)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /events/%s: got %d, want 200", id, rec.Code)
		}
		var v map[string]any
		mustDo(t, json.Unmarshal(rec.Body.Bytes(), &v))
		return v
	}

	on := get(ongoing.ID)
	if on["endTsMs"] != nil {
		t.Errorf("ongoing endTsMs = %v, want null", on["endTsMs"])
	}
	if on["ongoing"] != true || on["endTs"] == nil {
		t.Errorf("ongoing: ongoing=%v endTs=%v, want true + populated legacy endTs", on["ongoing"], on["endTs"])
	}
	fin := get(done.ID)
	if fin["endTsMs"] == nil || int64(fin["endTsMs"].(float64)) != T+5000 {
		t.Errorf("finalized endTsMs = %v, want %d", fin["endTsMs"], T+5000)
	}
}

// TestEventDetectionsEndpoint covers GET /events/{id}/detections returning the
// event's addressable objects, and camera-ACL enforcement on the event routes.
func TestEventDetectionsEndpoint(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	viewer := &store.User{ID: "v1", Email: "v@t", Name: "V", Role: "viewer"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateUser(viewer))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	mustDo(t, s.st.SetUserCameras(viewer.ID, []string{})) // no cameras

	ev := &store.Event{CameraID: "cam1", StartTS: 1_700_000_000_000}
	mustDo(t, s.st.InsertEvent(ev))
	mustDo(t, s.st.SaveEventDetections(ev.ID, "person", "1 person", []store.EventObject{
		{Label: "person", Confidence: 0.9, X: 0.1, Y: 0.1, W: 0.2, H: 0.3},
	}))

	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}
	viewerC := cred{"viewer", cookieCred(sessionCookieFor(t, s.am, viewer))}

	rec := s.reqAs("GET", "/api/v1/events/"+ev.ID+"/detections", adminC)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin detections: got %d, want 200", rec.Code)
	}
	var objs []store.EventObject
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &objs))
	if len(objs) != 1 || objs[0].ID == 0 || objs[0].Label != "person" {
		t.Fatalf("detections = %+v, want one addressable person object", objs)
	}

	// A viewer with no camera ACL cannot reach the event or its detections.
	for _, path := range []string{"/api/v1/events/" + ev.ID, "/api/v1/events/" + ev.ID + "/detections"} {
		if rec := s.reqAs("GET", path, viewerC); rec.Code != http.StatusForbidden {
			t.Errorf("viewer GET %s: got %d, want 403", path, rec.Code)
		}
	}
}

// TestCoreScopeEnforcement pins A3 across the CORE routes: a no-scope token is
// denied, and each scope opens exactly its routes.
func TestCoreScopeEnforcement(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))

	mk := func(scopes []string) cred {
		_, tok, err := s.st.CreateAPIToken(admin.ID, "t", 0, scopes, nil)
		mustDo(t, err)
		return cred{"token", bearerCred(tok)}
	}
	noScope := mk(nil)
	camsRead := mk([]string{ScopeCamerasRead})
	evRead := mk([]string{ScopeEventsRead})

	check := func(c cred, path string, want int) {
		if rec := s.reqAs("GET", path, c); rec.Code != want {
			t.Errorf("%s → %d, want %d", path, rec.Code, want)
		}
	}
	// Empty-scope token: denied on every scoped CORE route.
	check(noScope, "/api/v1/cameras", http.StatusForbidden)
	check(noScope, "/api/v1/events", http.StatusForbidden)
	// cameras:read opens /cameras but not /events.
	check(camsRead, "/api/v1/cameras", http.StatusOK)
	check(camsRead, "/api/v1/events", http.StatusForbidden)
	// events:read opens /events but not /cameras.
	check(evRead, "/api/v1/events", http.StatusOK)
	check(evRead, "/api/v1/cameras", http.StatusForbidden)
}
