package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// serveCancelled drives an SSE handler to completion: the handler writes its
// synchronous replay+snapshot, then its select loop sees the already-cancelled
// context and returns. The recorder captures everything written.
func serveCancelled(s *Server, req *http.Request) *httptest.ResponseRecorder {
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

// TestFeedResumeReplay covers change-feed resume: connecting with a
// Last-Event-ID replays durable events with a greater seq as event.created
// frames carrying an SSE id line, and nothing at or below the cursor.
func TestFeedResumeReplay(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	const T = int64(1_700_000_000_000)
	for i := 0; i < 3; i++ { // seq 1,2,3
		mustDo(t, s.st.InsertEvent(&store.Event{CameraID: "cam1", StartTS: T + int64(i)}))
	}

	req := httptest.NewRequest("GET", "/api/v1/feed", nil)
	adminC.apply(req)
	req.Header.Set("Last-Event-ID", "1")
	body := serveCancelled(s, req).Body.String()

	if !strings.Contains(body, "id: 2") || !strings.Contains(body, "id: 3") {
		t.Fatalf("resume did not replay seq 2 and 3:\n%s", body)
	}
	if strings.Contains(body, "id: 1") {
		t.Fatalf("resume replayed seq 1, which is at/below the cursor:\n%s", body)
	}
	if !strings.Contains(body, "event.created") {
		t.Fatalf("replayed frames are not event.created:\n%s", body)
	}
}

// TestFeedResetOnPrunedCursor covers the reset path: when the client's cursor
// predates the oldest retained event, the server sends reset (refetch), not a
// partial replay.
func TestFeedResetOnPrunedCursor(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	const T = int64(1_700_000_000_000)
	var ids []string
	for i := 0; i < 3; i++ { // seq 1,2,3
		e := &store.Event{CameraID: "cam1", StartTS: T + int64(i)}
		mustDo(t, s.st.InsertEvent(e))
		ids = append(ids, e.ID)
	}
	// Prune seq 1 and 2, so the oldest retained is seq 3.
	mustDo(t, s.st.DeleteEvent(ids[0]))
	mustDo(t, s.st.DeleteEvent(ids[1]))

	req := httptest.NewRequest("GET", "/api/v1/feed", nil)
	adminC.apply(req)
	req.Header.Set("Last-Event-ID", "1") // 1 < oldest(3)-1
	body := serveCancelled(s, req).Body.String()

	if !strings.Contains(body, `"type":"reset"`) {
		t.Fatalf("expected a reset for a pruned cursor:\n%s", body)
	}
	if strings.Contains(body, "event.created") {
		t.Fatalf("reset must not be accompanied by a partial replay:\n%s", body)
	}
}

// TestFeedScopeRequired covers the subscribe-time scope gate: a token without
// events:read is refused before the stream opens.
func TestFeedScopeRequired(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))

	_, noScope, err := s.st.CreateAPIToken(admin.ID, "noscope", 0, nil, nil)
	mustDo(t, err)
	if rec := s.reqAs("GET", "/api/v1/feed", cred{"token", bearerCred(noScope)}); rec.Code != http.StatusForbidden {
		t.Fatalf("token without events:read: got %d, want 403", rec.Code)
	}

	_, withScope, err := s.st.CreateAPIToken(admin.ID, "ev", 0, []string{ScopeEventsRead}, nil)
	mustDo(t, err)
	// With the scope, the connection is authorized (drives to completion via the
	// cancelled-context helper rather than blocking).
	req := httptest.NewRequest("GET", "/api/v1/feed", nil)
	bearerCred(withScope)(req)
	if rec := serveCancelled(s, req); rec.Code != http.StatusOK {
		t.Fatalf("token with events:read: got %d, want 200", rec.Code)
	}
}
