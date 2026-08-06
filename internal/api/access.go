package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/auth"
)

// camScope describes how a route is scoped to cameras.
type camScope int

const (
	camScopeNone  camScope = iota // not camera-scoped
	camScopeParam                  // scoped to the {id} route param (403 if the caller can't see it)
)

// access declares what a route requires. It is attached to a route only through
// apiRouter (the sanctioned registration surface); a route reachable without one
// crashes the process at boot (see assertAllRoutesDeclared) — the fail-closed
// backstop. The zero value means "authenticated, no further requirement".
type access struct {
	public      bool      // no authentication required
	admin       bool      // require role == admin
	sessionOnly bool      // reject class == token (session/cli only) — mechanism for 1.4
	scopes      []string  // token must hold all of these (unused until Phase 3)
	camera      camScope // per-camera gate
}

// Spec shorthands used at registration. Camera-scoped and scope-bearing specs
// are built inline where needed.
//
// authedCam/adminCam (camScopeParam) cover routes whose camera IS the {id} path
// param — enforce reads it and applies the ACL centrally. FOUR routes scope by
// camera WITHOUT a {id} param, so they KEEP an in-handler allowedCamera check and
// declare a non-camera spec. The boot guard proves a route HAS a spec, not that
// the spec is CORRECT: a camera-touching route wrongly declared with no camScope
// would pass the guard and leak. These four are the shape that mistake takes —
// read them before declaring a camera-related route without camScopeParam:
//   - GET  /cameras               (handleListCameras)  — filters the LIST to the
//     caller's ACL; a gate would be wrong (you return fewer rows, not a 403).
//   - GET  /events                (handleEventsAll)    — same list-filter shape.
//   - GET  /events/{id}/thumb.jpg (handleEventThumb)   — camera comes from the
//     event row (e.CameraID), not a path param.
//   - POST /export                (handleCreateExport) — cameras come from the
//     request body and are validated per-id.
var (
	pub       = access{public: true}                       // no auth
	authed    = access{}                                   // any authenticated principal
	admin     = access{admin: true}                        // admin only
	authedCam = access{camera: camScopeParam}                // authed + camera-scoped
	adminCam  = access{admin: true, camera: camScopeParam}   // admin + camera-scoped

	// Session-only specs reject class=token regardless of scope: these operations
	// are secret-exfil, destructive/irreversible, availability, or escalation
	// vectors and require full user authority (a cookie session or the CLI token).
	authedSessionOnly = access{sessionOnly: true}
	adminSessionOnly  = access{admin: true, sessionOnly: true}
)

// Scoped-spec helpers (Phase 3): every non-public, non-session-only route names
// the scope(s) a token must hold. Session and CLI credentials bypass the scope
// check, so these only constrain class=token — and an empty-scope token is
// denied on any scoped route (A3). `*Cam` variants add the {id} camera gate;
// `admin*` variants also require the token's owner to be an admin.
func scoped(scopes ...string) access         { return access{scopes: scopes} }
func scopedCam(scopes ...string) access      { return access{scopes: scopes, camera: camScopeParam} }
func adminScoped(scopes ...string) access    { return access{admin: true, scopes: scopes} }
func adminScopedCam(scopes ...string) access { return access{admin: true, scopes: scopes, camera: camScopeParam} }

// apiRouter is the only sanctioned way to register an API route: every method
// takes an access spec, records the route in the declared set (for the boot-time
// guard), and wraps the handler in the enforcing middleware.
type apiRouter struct {
	s *Server
	r chi.Router
}

func (s *Server) apiGroup(r chi.Router) *apiRouter { return &apiRouter{s: s, r: r} }

func (g *apiRouter) method(method, path string, a access, h http.HandlerFunc) {
	g.s.declaredRoutes[method+" "+path] = true
	g.r.With(g.s.enforce(a)).MethodFunc(method, path, h)
}

func (g *apiRouter) Get(path string, a access, h http.HandlerFunc)    { g.method(http.MethodGet, path, a, h) }
func (g *apiRouter) Post(path string, a access, h http.HandlerFunc)   { g.method(http.MethodPost, path, a, h) }
func (g *apiRouter) Put(path string, a access, h http.HandlerFunc)    { g.method(http.MethodPut, path, a, h) }
func (g *apiRouter) Delete(path string, a access, h http.HandlerFunc) { g.method(http.MethodDelete, path, a, h) }

// enforce is the per-route authorization middleware. Authentication comes from
// am.Context (installed at the router root); this layer decides admissibility.
func (s *Server) enforce(a access) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := auth.FromContext(r.Context())
			if !a.public {
				if c == nil {
					writeError(w, http.StatusUnauthorized, "authentication required")
					return
				}
				if a.admin && c.Role != "admin" {
					writeError(w, http.StatusForbidden, "administrator access required")
					return
				}
				if a.sessionOnly && c.Class == auth.ClassToken {
					writeError(w, http.StatusForbidden, "not available to API tokens")
					return
				}
				if c.Class == auth.ClassToken {
					for _, need := range a.scopes {
						if !hasScope(c.Scopes, need) {
							writeErrorCode(w, http.StatusForbidden, "forbidden.scope", "missing required scope: "+need)
							return
						}
					}
				}
			}
			if a.camera == camScopeParam && !s.allowedCamera(r, chi.URLParam(r, "id")) {
				writeErrorCode(w, http.StatusForbidden, "forbidden.camera", "access denied")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func hasScope(have []string, need string) bool {
	for _, s := range have {
		if s == need {
			return true
		}
	}
	return false
}

// assertAllRoutesDeclared is the fail-closed backstop: after the router is built
// it walks the whole tree and panics (refusing to boot) if any /api or /api/v1
// route was registered without going through apiRouter. An undeclared route thus
// cannot reach production — strictly earlier than any request-time check.
func (s *Server) assertAllRoutesDeclared(root chi.Router) {
	err := chi.Walk(root, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		rel, ok := apiRelPath(route)
		if !ok {
			return nil // outside the API surface (SPA, share viewer) — not guarded
		}
		if !s.declaredRoutes[method+" "+rel] {
			return fmt.Errorf("%s %s registered without an Access descriptor (bypassed apiGroup)", method, route)
		}
		return nil
	})
	if err != nil {
		panic("fail-closed authorization guard: " + err.Error())
	}
}

// apiRelPath strips the /api or /api/v1 mount prefix, returning the mount-relative
// pattern and whether the route is part of the API surface at all.
func apiRelPath(route string) (string, bool) {
	for _, p := range []string{"/api/v1", "/api"} { // check the longer prefix first
		if route == p {
			return "/", true
		}
		if strings.HasPrefix(route, p+"/") {
			return strings.TrimPrefix(route, p), true
		}
	}
	return "", false
}
