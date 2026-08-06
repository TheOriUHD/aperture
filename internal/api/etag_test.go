package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestIfMatchPolicy covers the four required If-Match cases on a config write:
// token-without → 428, token-stale → 412, token-matching → 200, session-without
// → 200. Cameras are the representative resource.
func TestIfMatchPolicy(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "Orig"}))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	_, tok, err := s.st.CreateAPIToken(admin.ID, "ext", 0, []string{ScopeCamerasWrite}, nil)
	mustDo(t, err)
	tokenC := cred{"token", bearerCred(tok)}

	put := func(c cred, ifMatch string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", "/api/v1/cameras/cam1", strings.NewReader(`{"name":"Renamed"}`))
		c.apply(r)
		if ifMatch != "" {
			r.Header.Set("If-Match", ifMatch)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec
	}

	// GET exposes the current ETag.
	get := s.reqAs("GET", "/api/v1/cameras/cam1", adminC)
	etag := get.Header().Get("ETag")
	if etag == "" {
		t.Fatal("GET /cameras/{id} did not set an ETag header")
	}

	// 1. token, no If-Match → 428.
	if rec := put(tokenC, ""); rec.Code != http.StatusPreconditionRequired {
		t.Errorf("token without If-Match: got %d, want 428", rec.Code)
	}
	// 2. token, stale If-Match → 412.
	if rec := put(tokenC, `"stale00000000000000000000000000"`); rec.Code != http.StatusPreconditionFailed {
		t.Errorf("token stale If-Match: got %d, want 412", rec.Code)
	}
	// 3. token, matching If-Match → 200 (and a fresh ETag on the response).
	rec := put(tokenC, etag)
	if rec.Code != http.StatusOK {
		t.Fatalf("token matching If-Match: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if newE := rec.Header().Get("ETag"); newE == "" || newE == etag {
		t.Errorf("update response should carry a fresh ETag (was %q, now %q)", etag, newE)
	}
	// 4. session, no If-Match → 200 (optional for sessions; last-writer-wins).
	if rec := put(adminC, ""); rec.Code != http.StatusOK {
		t.Errorf("session without If-Match: got %d, want 200", rec.Code)
	}
}

// TestIfMatchSessionEnforcedWhenSent pins the behavior 5.2 turns on in the web
// app. The web is a cookie-session client; once it replays the ETag it captured
// from a GET as If-Match on config writes, a matching tag must still succeed (no
// regression) and a stale tag must be rejected (the new optimistic-concurrency
// protection). TestIfMatchPolicy only covers a session that OMITS If-Match, so
// this is the missing half — mirroring the exact client round-trip: read the tag
// from the GET response header, replay it, then find it stale after the write.
func TestIfMatchSessionEnforcedWhenSent(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "Orig"}))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	put := func(ifMatch string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", "/api/v1/cameras/cam1", strings.NewReader(`{"name":"Renamed"}`))
		adminC.apply(r)
		r.Header.Set("If-Match", ifMatch)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec
	}

	// The client captures the ETag from the GET response header (setETag).
	etag := s.reqAs("GET", "/api/v1/cameras/cam1", adminC).Header().Get("ETag")
	if etag == "" {
		t.Fatal("GET did not expose an ETag header for the client to capture")
	}
	// Session + matching If-Match → 200 (the write must not break), and the reply
	// carries the next ETag the client will replay.
	rec := put(etag)
	if rec.Code != http.StatusOK {
		t.Fatalf("session matching If-Match: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if next := rec.Header().Get("ETag"); next == "" || next == etag {
		t.Fatalf("write should echo a fresh ETag for the next write (was %q, now %q)", etag, next)
	}
	// Session + the now-consumed (stale) If-Match → 412: a concurrent editor's
	// change is caught instead of being silently clobbered.
	if rec := put(etag); rec.Code != http.StatusPreconditionFailed {
		t.Errorf("session stale If-Match: got %d, want 412", rec.Code)
	}
}

// TestIfMatchStar covers If-Match: * (matches any existing resource).
func TestIfMatchStar(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "Orig"}))
	_, tok, err := s.st.CreateAPIToken(admin.ID, "ext", 0, []string{ScopeCamerasWrite}, nil)
	mustDo(t, err)

	r := httptest.NewRequest("PUT", "/api/v1/cameras/cam1", strings.NewReader(`{"name":"X"}`))
	bearerCred(tok)(r)
	r.Header.Set("If-Match", "*")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("If-Match: * on an existing resource: got %d, want 200", rec.Code)
	}
}
