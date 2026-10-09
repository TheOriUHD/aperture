package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestSegmentsScopeRequired covers T2.3: /segments now requires segments:read
// for token credentials. Sessions/CLI (all scopes) are unaffected; a token must
// hold the scope, and the scope check precedes the camera check.
func TestSegmentsScopeRequired(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s, admin))}

	// Session (implicitly all scopes) → 200.
	if rec := s.reqAs("GET", "/api/v1/cameras/cam1/segments", adminC); rec.Code != http.StatusOK {
		t.Fatalf("admin session GET /segments: got %d, want 200", rec.Code)
	}

	// Token granted the camera but NOT segments:read → 403 forbidden.scope.
	_, noScope, err := s.st.CreateAPIToken(admin.ID, "noscope", 0, nil, []string{"cam1"})
	mustDo(t, err)
	if rec := s.reqAs("GET", "/api/v1/cameras/cam1/segments", cred{"token", bearerCred(noScope)}); rec.Code != http.StatusForbidden {
		t.Fatalf("token without segments:read: got %d, want 403", rec.Code)
	}

	// Token with segments:read + camera grant → 200.
	_, withScope, err := s.st.CreateAPIToken(admin.ID, "seg", 0, []string{ScopeSegmentsRead}, []string{"cam1"})
	mustDo(t, err)
	if rec := s.reqAs("GET", "/api/v1/cameras/cam1/segments", cred{"token", bearerCred(withScope)}); rec.Code != http.StatusOK {
		t.Fatalf("token with segments:read: got %d, want 200", rec.Code)
	}
}

// TestSegmentsKeysetPaging covers T2.3 keyset paging over the (start_ts,id)
// cursor — including a start_ts tie, which the id tiebreaker must resolve
// without duplicating or skipping — while the default (no cursor/limit) path
// still returns the whole range for the render path.
func TestSegmentsKeysetPaging(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s, admin))}

	const T = int64(1_700_000_000_000)
	starts := []int64{T, T + 10, T + 10, T + 20, T + 30, T + 40, T + 50} // one tie at T+10
	for _, st0 := range starts {
		mustDo(t, s.st.InsertSegment(&store.Segment{CameraID: "cam1", Path: "x", StartTS: st0, EndTS: st0 + 5}))
	}
	rng := fmt.Sprintf("from=%d&to=%d", T-1, T+1_000)

	// Default path: whole range, no truncation (render-path compatibility).
	rec := s.reqAs("GET", "/api/v1/cameras/cam1/segments?"+rng, adminC)
	if rec.Code != http.StatusOK {
		t.Fatalf("default segments: got %d, want 200", rec.Code)
	}
	var all []map[string]any
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &all))
	if len(all) != 7 {
		t.Fatalf("default path returned %d segments, want all 7", len(all))
	}

	// Keyset path: page through with limit=3, following X-Next-Cursor.
	seen := map[string]int{}
	cursor := ""
	pages := 0
	for {
		url := "/api/v1/cameras/cam1/segments?" + rng + "&limit=3"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		rec := s.reqAs("GET", url, adminC)
		if rec.Code != http.StatusOK {
			t.Fatalf("page %d: got %d, want 200", pages, rec.Code)
		}
		var out []map[string]any
		mustDo(t, json.Unmarshal(rec.Body.Bytes(), &out))
		for _, seg := range out {
			seen[seg["id"].(string)]++
		}
		cursor = rec.Header().Get("X-Next-Cursor")
		pages++
		if cursor == "" {
			break
		}
		if pages > 20 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 7 {
		t.Fatalf("keyset paging saw %d distinct segments, want 7", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("segment %s seen %d times, want exactly 1 (tie must not dup/skip)", id, n)
		}
	}
}
