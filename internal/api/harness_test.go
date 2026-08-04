package api

// HTTP-level test harness for the API. It mounts the real router
// (Server.routes()) against a temp store and drives requests through httptest,
// so authentication, RBAC, and per-camera ACL are exercised end-to-end. This is
// the baseline the v1 authorization layer (Phase 1.3/1.4) extends.
//
// Credential classes are modeled as `cred` decorators, so adding the scoped
// `token` class and per-scope rows in Phase 1.4 means adding creds + table rows,
// not restructuring the matrix.

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/detect"
	"github.com/TheOriUHD/aperture/internal/store"
)

// testCLIToken is the synthetic-admin CLI bearer token wired into the harness's
// auth manager; the `cli` credential class in the matrix presents it.
const testCLIToken = "test-cli-token"

// newTestServer builds a Server with just the fields the exercised routes need.
// Other subsystems (recorder, streams, exporter, …) stay nil; tests must only
// hit routes whose handlers don't dereference them.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	am := auth.NewManager(
		[]byte("test-jwt-secret-0123456789abcdef"),
		[]byte("test-stream-secret-0123456789abc"),
		[]byte(testCLIToken),
		false,
	)
	cfg := &config.Config{DataDir: dir, RecordingsDir: filepath.Join(dir, "recordings")}
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &Server{
		st:        st,
		am:        am,
		cfg:       cfg,
		detect:    detect.New(st, nil, dir, lg),
		log:       lg,
		startedAt: time.Now(),
	}
	s.router = s.routes()
	return s
}

func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
}

// cred is a credential class applied to a test request. Phase 1.4's scoped
// `token` class plugs in here as another decorator.
type cred struct {
	name  string
	apply func(*http.Request) // nil = anonymous
}

func cookieCred(sessionToken string) func(*http.Request) {
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "aperture_session", Value: sessionToken})
	}
}

func bearerCred(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

// sessionCookieFor mints a signed session JWT for a user (the cookie value the
// web app would carry).
func sessionCookieFor(t *testing.T, am *auth.Manager, u *store.User) string {
	t.Helper()
	tok, err := am.IssueSession(u.ID, u.Email, u.Name, u.Role, time.Hour)
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	return tok
}

// reqAs sends method+path with the given credential class applied.
func (s *Server) reqAs(method, path string, c cred) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if c.apply != nil {
		c.apply(r)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}
