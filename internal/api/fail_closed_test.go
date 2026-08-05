package api

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestFailClosed_UndeclaredRouteRefusesBoot is the single most load-bearing test
// in the API contract: a route registered on the REAL router WITHOUT going
// through the Access wrapper must not be reachable. The guard is boot-time — an
// undeclared route makes routes() panic (refuse to build), so it can never reach
// production, strictly earlier than any request-time check.
//
// This keeps the authorization choke point from eroding as endpoints are added
// over the coming year.
func TestFailClosed_UndeclaredRouteRefusesBoot(t *testing.T) {
	s := newTestServerNoRouter(t)
	// A route that bypasses the Access wrapper entirely (raw chi registration).
	s.extraAPIRoutes = func(r chi.Router) {
		r.Get("/__undeclared__", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	}
	defer func() {
		if recover() == nil {
			t.Fatal("FAIL-OPEN: routes() built with an undeclared route; the boot-time guard must refuse to build")
		}
	}()
	_ = s.routes() // must panic — an /api route skipped apiGroup
}

// TestRealRouterBootsClean pins that the guard is reachable from `go test`, not
// only from nvrd startup: building the PRODUCTION route set (what every harness
// test does via newTestServer → routes()) must not panic. So if a real route is
// ever added without a spec, this — and every other harness test — fails the
// suite, catching it before deploy rather than when nvrd refuses to start on a
// recorder.
func TestRealRouterBootsClean(t *testing.T) {
	_ = newTestServer(t) // routes() runs the boot guard over every real route; panics ⇒ test fails
}
