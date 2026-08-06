package api

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/TheOriUHD/aperture/internal/audit"
	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/automate"
	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/detect"
	"github.com/TheOriUHD/aperture/internal/export"
	"github.com/TheOriUHD/aperture/internal/ha"
	"github.com/TheOriUHD/aperture/internal/logbuf"
	"github.com/TheOriUHD/aperture/internal/motion"
	"github.com/TheOriUHD/aperture/internal/notify"
	"github.com/TheOriUHD/aperture/internal/oidc"
	"github.com/TheOriUHD/aperture/internal/record"
	"github.com/TheOriUHD/aperture/internal/secret"
	"github.com/TheOriUHD/aperture/internal/storage"
	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
	"github.com/TheOriUHD/aperture/internal/watchdog"
	"github.com/TheOriUHD/aperture/internal/weather"
	"github.com/TheOriUHD/aperture/internal/webhook"
)

const sessionTTL = 7 * 24 * time.Hour

// Server wires the HTTP API and the embedded web UI.
type Server struct {
	cfg       *config.Config
	st        *store.Store
	am        *auth.Manager
	sm        *stream.Manager
	rec       *record.Recorder
	det       *motion.Detector
	detect    *detect.Engine
	webhook   *webhook.Dispatcher
	automate  *automate.Engine
	ha        *ha.Publisher
	oidc      *oidc.Authenticator
	audit     *audit.Logger
	exp       *export.Manager
	notify    *notify.Engine
	weather   *weather.Poller
	watchdog  *watchdog.Watchdog
	logs      *logbuf.Ring
	waSess    *waSessions
	registry  *storage.Registry
	cipher    *secret.Cipher
	reconcile func() // triggers an immediate retention/reservation pass (set at startup)
	startedAt time.Time
	log       *slog.Logger
	router    http.Handler

	// extraAPIRoutes, if non-nil, registers additional routes inside the API
	// group. Test-only seam for exercising the fail-closed authorization guard
	// with a route that bypasses the Access wrapper; always nil in production.
	extraAPIRoutes func(chi.Router)

	// declaredRoutes records every (METHOD path) registered through apiRouter,
	// consulted by the boot-time fail-closed guard (assertAllRoutesDeclared).
	declaredRoutes map[string]bool

	// limiter is the per-(class,id) request rate limiter (T2.6).
	limiter *rateLimiter
}

// SetStorage wires the storage-backend registry and credential cipher used for
// network storage locations. Called once at startup after construction.
func (s *Server) SetStorage(reg *storage.Registry, c *secret.Cipher) {
	s.registry = reg
	s.cipher = c
}

// SetReconcile wires a callback that runs an immediate retention/reservation pass,
// so allocation changes take effect without waiting for the janitor's next tick.
func (s *Server) SetReconcile(fn func()) { s.reconcile = fn }

// triggerReconcile runs the reconcile callback in the background if wired.
func (s *Server) triggerReconcile() {
	if s.reconcile != nil {
		go s.reconcile()
	}
}

// NewServer builds a Server and its routes.
func NewServer(cfg *config.Config, st *store.Store, am *auth.Manager, sm *stream.Manager, rec *record.Recorder, det *motion.Detector, detectEng *detect.Engine, webhookDisp *webhook.Dispatcher, automateEng *automate.Engine, haPub *ha.Publisher, oidcAuth *oidc.Authenticator, auditLog *audit.Logger, exp *export.Manager, notifyEng *notify.Engine, weatherPoller *weather.Poller, wd *watchdog.Watchdog, logs *logbuf.Ring, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, st: st, am: am, sm: sm, rec: rec, det: det, detect: detectEng, webhook: webhookDisp, automate: automateEng, ha: haPub, oidc: oidcAuth, audit: auditLog, exp: exp, notify: notifyEng, weather: weatherPoller, watchdog: wd, logs: logs, waSess: newWASessions(), startedAt: time.Now(), log: log}
	s.limiter = newRateLimiter()
	s.router = s.routes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }

// logRequests logs API mutations and any error responses, so we can see exactly
// what reaches the server.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		if r.Method != http.MethodGet || ww.Status() >= 400 {
			s.log.Info("api", "method", r.Method, "path", r.URL.Path,
				"status", ww.Status(), "ms", time.Since(start).Milliseconds())
		}
	})
}

// apiVersionHeaders stamps every API response with the served contract version.
// On the unversioned /api alias it also emits an RFC 8594 Deprecation marker and
// a successor-version Link to /api/v1; the alias is retired in Phase 5.4.
func (s *Server) apiVersionHeaders(version string, deprecated bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Aperture-Api-Version", version)
			if deprecated {
				w.Header().Set("Deprecation", "true")
				w.Header().Set("Link", `</api/v1>; rel="successor-version"`)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.logRequests)
	r.Use(s.am.Context)

	s.declaredRoutes = map[string]bool{}
	apiRoutes := func(r chi.Router) {
		// Rate-limit (per class+principal) then cap the request body, before any
		// handler runs. Both mounts (/api, /api/v1) get these via apiRoutes.
		r.Use(s.rateLimit)
		r.Use(s.limitBody)
		g := s.apiGroup(r)

		// Public — no authentication.
		g.Get("/system/health", pub, s.handleHealth)
		g.Get("/system/version", pub, s.handleVersion)
		g.Get("/version", pub, s.handleAPIVersion) // API contract version (pre-auth negotiation)
		g.Get("/setup/status", pub, s.handleSetupStatus)
		g.Post("/setup", pub, s.handleSetup)
		g.Post("/auth/login", pub, s.handleLogin)
		g.Post("/auth/login/2fa", pub, s.handleLogin2FA)
		g.Post("/auth/passkey/login/begin", pub, s.handlePasskeyLoginBegin)
		g.Post("/auth/passkey/login/finish", pub, s.handlePasskeyLoginFinish)
		g.Post("/auth/logout", pub, s.handleLogout)
		g.Get("/auth/oidc/status", pub, s.handleOIDCStatus)
		g.Get("/auth/oidc/login", pub, s.handleOIDCLogin)
		g.Get("/auth/oidc/callback", pub, s.handleOIDCCallback)
		g.Get("/public/event-thumb", pub, s.handlePublicThumb)
		// Live stream WebSocket (authorized via a short-lived stream token in the query).
		g.Get("/cameras/{id}/ws", pub, s.handleStreamWS)

		// Authenticated. Camera-scoped routes use authedCam so the per-camera ACL
		// is enforced in one place; handlers still hold their allowedCamera call
		// until the matrix pins each route (removed in the same task).
		g.Get("/auth/me", authed, s.handleMe)
		g.Get("/capabilities", authed, s.handleCapabilities)
		g.Post("/auth/password", authed, s.handleChangePassword)
		g.Post("/auth/2fa/setup", authed, s.handle2FASetup)
		g.Post("/auth/2fa/enable", authed, s.handle2FAEnable)
		g.Post("/auth/2fa/disable", authed, s.handle2FADisable)
		g.Post("/auth/passkey/register/begin", authed, s.handlePasskeyRegisterBegin)
		g.Post("/auth/passkey/register/finish", authed, s.handlePasskeyRegisterFinish)
		g.Get("/auth/passkeys", authed, s.handleListPasskeys)
		g.Delete("/auth/passkeys/{id}", authed, s.handleDeletePasskey)
		g.Get("/users/me/preferences", authed, s.handleGetPrefs)
		g.Put("/users/me/preferences", authed, s.handleSetPrefs)
		g.Get("/cameras", authed, s.handleListCameras)
		g.Get("/camera-groups", authed, s.handleListGroups)
		g.Get("/cameras/{id}", authedCam, s.handleGetCamera)
		g.Get("/cameras/{id}/stream-token", authedCam, s.handleStreamToken)
		g.Get("/cameras/{id}/snapshot.jpg", authedCam, s.handleSnapshot)
		g.Get("/cameras/{id}/frame", authedCam, s.handleFrame)
		g.Get("/cameras/{id}/segments", access{scopes: []string{ScopeSegmentsRead}, camera: camScopeParam}, s.handleSegmentsList)
		g.Get("/cameras/{id}/coverage", authedCam, s.handleCoverage)
		g.Get("/cameras/{id}/events", authedCam, s.handleCameraEvents)
		g.Get("/cameras/{id}/hls", authedCam, s.handleHLS)
		g.Get("/cameras/{id}/seg/{seg}", authedCam, s.handleSegmentFile)
		g.Delete("/cameras/{id}/recordings", authedCam, s.handleDeleteRecordings) // + internal admin check
		g.Get("/events", authed, s.handleEventsAll)
			g.Get("/feed", access{scopes: []string{ScopeEventsRead}}, s.handleFeed) // T2.5 change feed (SSE)
		g.Get("/events/{id}/thumb.jpg", authed, s.handleEventThumb) // camera resolved from the event row (hand-checked)
		g.Post("/export", authed, s.withIdempotency(s.handleCreateExport))             // cameras validated from the body (hand-checked)
		g.Get("/export", authed, s.handleListExports)
		g.Get("/export/{id}", authed, s.handleGetExport)
		g.Get("/export/{id}/download", authed, s.handleDownloadExport)
		g.Delete("/export/{id}", authed, s.handleDeleteExport)
		g.Post("/shares", authed, s.withIdempotency(s.handleCreateShare))
		g.Get("/shares", authed, s.handleListShares)
		g.Delete("/shares/{id}", authed, s.handleDeleteShare)
		g.Get("/notify/channels", authed, s.handleListChannels)
		g.Post("/notify/channels", authed, s.handleCreateChannel)
		g.Put("/notify/channels/{id}", authed, s.handleUpdateChannel)
		g.Delete("/notify/channels/{id}", authed, s.handleDeleteChannel)
		g.Post("/notify/channels/{id}/test", authed, s.handleTestChannel)
		g.Get("/notify/rules", authed, s.handleListRules)
		g.Post("/notify/rules", authed, s.handleCreateRule)
		g.Put("/notify/rules/{id}", authed, s.handleUpdateRule)
		g.Delete("/notify/rules/{id}", authed, s.handleDeleteRule)
		g.Post("/notify/rules/{id}/test", authed, s.handleTestRule)
		g.Get("/notify/stream", authed, s.handleNotifyStream)
		g.Get("/push/key", authed, s.handlePushKey)
		g.Post("/push/subscribe", authed, s.handlePushSubscribe)
		g.Post("/push/unsubscribe", authed, s.handlePushUnsubscribe)
		g.Get("/cameras/{id}/schedule", authedCam, s.handleGetSchedule)
		g.Get("/cameras/{id}/zones", authedCam, s.handleGetZones)
		g.Get("/system/storage", authed, s.handleStorage)
		g.Get("/system/bitrate", authed, s.handleBitrate)
		g.Get("/system/settings", authed, s.handleGetSettings)
		g.Get("/system/site", authed, s.handleGetSite)
		g.Get("/system/weather/now", authed, s.handleWeatherNow)
		g.Get("/system/weather/at", authed, s.handleWeatherAt)
		g.Get("/system/weather/range", authed, s.handleWeatherRange)
		g.Get("/system/astro", authed, s.handleAstro)
		// Personal API tokens (any user manages their own) + API docs.
		g.Get("/tokens", authedSessionOnly, s.handleListTokens)
		g.Post("/tokens", authedSessionOnly, s.handleCreateToken)
		g.Delete("/tokens/{id}", authedSessionOnly, s.handleDeleteToken)
		g.Get("/openapi.json", authed, s.handleOpenAPI)

		// Admin only.
		g.Post("/cameras", admin, s.handleCreateCamera)
		g.Put("/cameras/{id}", admin, s.handleUpdateCamera)
		g.Delete("/cameras/{id}", admin, s.handleDeleteCamera)
		g.Post("/cameras/reorder", admin, s.handleReorderCameras)
		g.Post("/camera-groups", admin, s.handleCreateGroup)
		g.Put("/camera-groups/{id}", admin, s.handleUpdateGroup)
		g.Delete("/camera-groups/{id}", admin, s.handleDeleteGroup)
		g.Put("/cameras/{id}/groups", admin, s.handleSetCameraGroups)
		g.Get("/users", adminSessionOnly, s.handleListUsers)
		g.Post("/users", adminSessionOnly, s.handleCreateUser)
		g.Put("/users/{id}", adminSessionOnly, s.handleUpdateUser)
		g.Delete("/users/{id}", adminSessionOnly, s.handleDeleteUser)
		g.Put("/cameras/{id}/schedule", admin, s.handleSetSchedule)
		g.Put("/cameras/{id}/zones", admin, s.handleSetZones)
		g.Put("/system/settings", admin, s.handleSetSettings)
		g.Put("/system/site", admin, s.handleSetSite)
		g.Get("/system/geocode", admin, s.handleGeocode)
		g.Get("/system/geo/resolve", admin, s.handleResolveGeo)
		g.Get("/system/weather", admin, s.handleGetWeather)
		g.Put("/system/weather", admin, s.handleSetWeather)
		g.Post("/system/purge", adminSessionOnly, s.handlePurge)
		g.Get("/system/status", admin, s.handleSystemStatus)
		g.Get("/system/metrics", admin, s.handleMetrics)
		g.Get("/system/diagnostics", admin, s.handleDiagnostics)
		g.Post("/cameras/test", admin, s.handleTestCamera)
		g.Get("/system/backup", adminSessionOnly, s.handleBackup)
		g.Post("/system/restore", adminSessionOnly, s.handleRestore)
		g.Post("/system/reindex", adminSessionOnly, s.handleReindex)
		g.Put("/system/storage", admin, s.handleSetStorageConfig)
		g.Put("/system/playback", admin, s.handleSetPlayback) // T2.2 runtime seek switch
			g.Get("/system/storage/disk", admin, s.handleDiskStats)
		g.Post("/system/storage/reserve", admin, s.handleReserveStorage)
		g.Get("/system/storage/locations", admin, s.handleListLocations)
		g.Post("/system/storage/locations", admin, s.handleCreateLocation)
		g.Post("/system/storage/locations/test", admin, s.handleTestLocation)
		g.Post("/system/storage/locations/allocate", admin, s.handleAllocateLocation)
		g.Post("/system/storage/locations/reorder", admin, s.handleReorderLocations)
		g.Post("/system/storage/locations/assign", admin, s.handleAssignCameras)
		g.Put("/system/storage/locations/{id}", admin, s.handleUpdateLocation)
		g.Delete("/system/storage/locations/{id}", admin, s.handleDeleteLocation)
		g.Post("/system/restart", adminSessionOnly, s.handleRestart)
		g.Get("/system/ha", admin, s.handleGetHA)
		g.Put("/system/ha", admin, s.handleSetHA)
		g.Get("/system/ha/sensors", admin, s.handleHASensors)
		g.Get("/system/oidc", admin, s.handleGetOIDC)
		g.Put("/system/oidc", admin, s.handleSetOIDC)
		g.Get("/system/detection", admin, s.handleGetDetection)
		g.Put("/system/detection", admin, s.handleSetDetection)
		g.Post("/system/detection/test", admin, s.handleTestDetection)
		g.Get("/webhooks", admin, s.handleListWebhooks)
		g.Post("/webhooks", admin, s.handleCreateWebhook)
		g.Put("/webhooks/{id}", admin, s.handleUpdateWebhook)
		g.Delete("/webhooks/{id}", admin, s.handleDeleteWebhook)
		g.Post("/webhooks/{id}/test", admin, s.handleTestWebhook)
		g.Get("/webhooks/{id}/deliveries", admin, s.handleWebhookDeliveries)
		g.Get("/automations", admin, s.handleListAutomations)
		g.Post("/automations", admin, s.handleCreateAutomation)
		g.Put("/automations/{id}", admin, s.handleUpdateAutomation)
		g.Delete("/automations/{id}", admin, s.handleDeleteAutomation)
		g.Post("/automations/{id}/test", admin, s.handleTestAutomation)
		g.Get("/system/audit", admin, s.handleGetAudit)
		g.Get("/system/audit/verify", admin, s.handleVerifyAudit)
	}

	// Mount the full API at both the unversioned alias and /api/v1. Both share
	// the same handlers — today v1 IS the one behavior (there are no external
	// clients yet); the versioning machinery exists so a future v2 can diverge
	// under the deprecation policy. The alias is retired in Phase 5.4.
	r.Route("/api", func(r chi.Router) {
		r.Use(s.apiVersionHeaders("v1", true))
		apiRoutes(r)
	})
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.apiVersionHeaders("v1", false))
		apiRoutes(r)
		if s.extraAPIRoutes != nil {
			s.extraAPIRoutes(r) // test-only: exercise the fail-closed guard
		}
	})

	// Public share viewer (chrome-less, token-gated).
	r.Get("/s/{token}", s.handleSharePage)
	r.Get("/s/{token}/media", s.handleShareMedia)

	// Everything else: the SPA.
	r.NotFound(s.spaHandler())

	// Fail-closed backstop: refuse to boot if any /api route was registered
	// without going through apiGroup (and thus carries no Access descriptor).
	s.assertAllRoutesDeclared(r)
	return r
}
