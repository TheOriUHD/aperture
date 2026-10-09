package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestRateLimiterRefill covers the token-bucket math with injected time.
func TestRateLimiterRefill(t *testing.T) {
	l := newRateLimiter()
	cfg := rateConfig{burst: 2, refill: 1} // capacity 2, 1 token/sec
	t0 := time.Unix(1000, 0)

	if !l.allow("k", cfg, t0) || !l.allow("k", cfg, t0) {
		t.Fatal("first two calls should pass (burst 2)")
	}
	if l.allow("k", cfg, t0) {
		t.Fatal("third call should fail (bucket empty)")
	}
	if !l.allow("k", cfg, t0.Add(time.Second)) {
		t.Fatal("after 1s one token should have refilled")
	}
	if l.allow("k", cfg, t0.Add(time.Second)) {
		t.Fatal("only one token should refill in 1s")
	}

	// A different key has its own independent bucket.
	if !l.allow("other", cfg, t0) {
		t.Fatal("independent key should have a full bucket")
	}

	// prune drops an idle bucket.
	l.prune(t0.Add(time.Hour), 15*time.Minute)
	if len(l.buckets) != 0 {
		t.Fatalf("prune left %d buckets, want 0", len(l.buckets))
	}
}

// TestBodyLimit413 covers the request-body cap: a JSON body over 1 MiB is
// refused 413 before the handler runs.
func TestBodyLimit413(t *testing.T) {
	s := newTestServer(t)
	big := bytes.NewReader(make([]byte, 2<<20)) // 2 MiB, Content-Length set by httptest
	r := httptest.NewRequest("POST", "/api/v1/auth/login", big)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("2 MiB body: got %d, want 413", rec.Code)
	}

	// A small body is not blocked by the cap (login fails auth, not size).
	small := bytes.NewReader([]byte(`{"email":"x","password":"y"}`))
	r = httptest.NewRequest("POST", "/api/v1/auth/login", small)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("small body wrongly rejected as too large")
	}
}

// TestReadBodyTimeoutNoRegression covers Item 5: the per-request read deadline
// on non-multipart bodies is best-effort (no-op on a recorder, no connection)
// and must not break the normal request path.
func TestReadBodyTimeoutNoRegression(t *testing.T) {
	s := newTestServer(t)
	small := bytes.NewReader([]byte(`{"email":"x","password":"y"}`))
	r := httptest.NewRequest("POST", "/api/v1/auth/login", small)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code == http.StatusRequestEntityTooLarge || rec.Code >= 500 {
		t.Fatalf("normal POST broke under the read-deadline middleware: got %d", rec.Code)
	}
}

// TestRateLimitPerClass covers T2.6: a token exhausting its bucket gets 429
// while a session (separate bucket) is unaffected.
func TestRateLimitPerClass(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	_, plain, err := s.st.CreateAPIToken(admin.ID, "ext", 0, []string{ScopeCamerasRead}, nil)
	mustDo(t, err)

	// Shrink the token bucket to 3 with no refill; leave the session bucket large.
	s.limiter.token = rateConfig{burst: 3, refill: 0}
	tokenC := cred{"token", bearerCred(plain)}
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s, admin))}

	var ok, limited int
	for i := 0; i < 6; i++ {
		switch s.reqAs("GET", "/api/v1/cameras", tokenC).Code {
		case http.StatusOK:
			ok++
		case http.StatusTooManyRequests:
			limited++
		}
	}
	if ok != 3 || limited != 3 {
		t.Fatalf("token: %d ok / %d limited, want 3/3 (burst 3, no refill)", ok, limited)
	}

	// The session bucket is untouched by the token's exhaustion.
	for i := 0; i < 6; i++ {
		if rec := s.reqAs("GET", "/api/v1/cameras", adminC); rec.Code != http.StatusOK {
			t.Fatalf("session request %d: got %d, want 200 (session bucket independent)", i, rec.Code)
		}
	}
}
