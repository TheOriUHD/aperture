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

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.logRequests)
	r.Use(s.am.Context)

	r.Route("/api", func(r chi.Router) {
		// Public.
		r.Get("/system/health", s.handleHealth)
		r.Get("/system/version", s.handleVersion)
		r.Get("/setup/status", s.handleSetupStatus)
		r.Post("/setup", s.handleSetup)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/login/2fa", s.handleLogin2FA)
		r.Post("/auth/passkey/login/begin", s.handlePasskeyLoginBegin)
		r.Post("/auth/passkey/login/finish", s.handlePasskeyLoginFinish)
		r.Post("/auth/logout", s.handleLogout)
		r.Get("/auth/oidc/status", s.handleOIDCStatus)
		r.Get("/auth/oidc/login", s.handleOIDCLogin)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)
		r.Get("/public/event-thumb", s.handlePublicThumb)

		// Live stream WebSocket (authorized via short-lived stream token in query).
		r.Get("/cameras/{id}/ws", s.handleStreamWS)

		// Authenticated.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Get("/auth/me", s.handleMe)
			r.Post("/auth/password", s.handleChangePassword)
			r.Post("/auth/2fa/setup", s.handle2FASetup)
			r.Post("/auth/2fa/enable", s.handle2FAEnable)
			r.Post("/auth/2fa/disable", s.handle2FADisable)
			r.Post("/auth/passkey/register/begin", s.handlePasskeyRegisterBegin)
			r.Post("/auth/passkey/register/finish", s.handlePasskeyRegisterFinish)
			r.Get("/auth/passkeys", s.handleListPasskeys)
			r.Delete("/auth/passkeys/{id}", s.handleDeletePasskey)
			r.Get("/users/me/preferences", s.handleGetPrefs)
			r.Put("/users/me/preferences", s.handleSetPrefs)
			r.Get("/cameras", s.handleListCameras)
			r.Get("/camera-groups", s.handleListGroups)
			r.Get("/cameras/{id}", s.handleGetCamera)
			r.Get("/cameras/{id}/stream-token", s.handleStreamToken)
			r.Get("/cameras/{id}/snapshot.jpg", s.handleSnapshot)
			r.Get("/cameras/{id}/frame", s.handleFrame)
			r.Get("/cameras/{id}/segments", s.handleSegmentsList)
			r.Get("/cameras/{id}/events", s.handleCameraEvents)
			r.Get("/cameras/{id}/hls", s.handleHLS)
			r.Get("/cameras/{id}/seg/{seg}", s.handleSegmentFile)
			r.Delete("/cameras/{id}/recordings", s.handleDeleteRecordings) // Playback "Remove" (admin, checked in handler)
			r.Get("/events", s.handleEventsAll)
			r.Get("/events/{id}/thumb.jpg", s.handleEventThumb)
			r.Post("/export", s.handleCreateExport)
			r.Get("/export", s.handleListExports)
			r.Get("/export/{id}", s.handleGetExport)
			r.Get("/export/{id}/download", s.handleDownloadExport)
			r.Delete("/export/{id}", s.handleDeleteExport)
			r.Post("/shares", s.handleCreateShare)
			r.Get("/shares", s.handleListShares)
			r.Delete("/shares/{id}", s.handleDeleteShare)
			r.Get("/notify/channels", s.handleListChannels)
			r.Post("/notify/channels", s.handleCreateChannel)
			r.Put("/notify/channels/{id}", s.handleUpdateChannel)
			r.Delete("/notify/channels/{id}", s.handleDeleteChannel)
			r.Post("/notify/channels/{id}/test", s.handleTestChannel)
			r.Get("/notify/rules", s.handleListRules)
			r.Post("/notify/rules", s.handleCreateRule)
			r.Put("/notify/rules/{id}", s.handleUpdateRule)
			r.Delete("/notify/rules/{id}", s.handleDeleteRule)
			r.Post("/notify/rules/{id}/test", s.handleTestRule)
			r.Get("/notify/stream", s.handleNotifyStream)
			r.Get("/push/key", s.handlePushKey)
			r.Post("/push/subscribe", s.handlePushSubscribe)
			r.Post("/push/unsubscribe", s.handlePushUnsubscribe)
			r.Get("/cameras/{id}/schedule", s.handleGetSchedule)
			r.Get("/cameras/{id}/zones", s.handleGetZones)
			r.Get("/system/storage", s.handleStorage)
			r.Get("/system/bitrate", s.handleBitrate)
			r.Get("/system/settings", s.handleGetSettings)
			r.Get("/system/site", s.handleGetSite)
			r.Get("/system/weather/now", s.handleWeatherNow)
			r.Get("/system/weather/at", s.handleWeatherAt)
			r.Get("/system/weather/range", s.handleWeatherRange)
			r.Get("/system/astro", s.handleAstro)
			// Personal API tokens (any user manages their own) + API docs.
			r.Get("/tokens", s.handleListTokens)
			r.Post("/tokens", s.handleCreateToken)
			r.Delete("/tokens/{id}", s.handleDeleteToken)
			r.Get("/openapi.json", s.handleOpenAPI)
		})

		// Admin-only.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Use(auth.RequireAdmin)
			r.Post("/cameras", s.handleCreateCamera)
			r.Put("/cameras/{id}", s.handleUpdateCamera)
			r.Delete("/cameras/{id}", s.handleDeleteCamera)
			r.Post("/cameras/reorder", s.handleReorderCameras)
			r.Post("/camera-groups", s.handleCreateGroup)
			r.Put("/camera-groups/{id}", s.handleUpdateGroup)
			r.Delete("/camera-groups/{id}", s.handleDeleteGroup)
			r.Put("/cameras/{id}/groups", s.handleSetCameraGroups)
			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)
			r.Put("/cameras/{id}/schedule", s.handleSetSchedule)
			r.Put("/cameras/{id}/zones", s.handleSetZones)
			r.Put("/system/settings", s.handleSetSettings)
			r.Put("/system/site", s.handleSetSite)
			r.Get("/system/geocode", s.handleGeocode)
			r.Get("/system/geo/resolve", s.handleResolveGeo)
			r.Get("/system/weather", s.handleGetWeather)
			r.Put("/system/weather", s.handleSetWeather)
			r.Post("/system/purge", s.handlePurge)
			r.Get("/system/status", s.handleSystemStatus)
			r.Get("/system/metrics", s.handleMetrics)
			r.Get("/system/diagnostics", s.handleDiagnostics)
			r.Post("/cameras/test", s.handleTestCamera)
			r.Get("/system/backup", s.handleBackup)
			r.Post("/system/restore", s.handleRestore)
			r.Post("/system/reindex", s.handleReindex)
			r.Put("/system/storage", s.handleSetStorageConfig)
			r.Get("/system/storage/disk", s.handleDiskStats)
			r.Post("/system/storage/reserve", s.handleReserveStorage)
			r.Get("/system/storage/locations", s.handleListLocations)
			r.Post("/system/storage/locations", s.handleCreateLocation)
			r.Post("/system/storage/locations/test", s.handleTestLocation)
			r.Post("/system/storage/locations/allocate", s.handleAllocateLocation)
			r.Post("/system/storage/locations/reorder", s.handleReorderLocations)
			r.Post("/system/storage/locations/assign", s.handleAssignCameras)
			r.Put("/system/storage/locations/{id}", s.handleUpdateLocation)
			r.Delete("/system/storage/locations/{id}", s.handleDeleteLocation)
			r.Post("/system/restart", s.handleRestart)
			r.Get("/system/ha", s.handleGetHA)
			r.Put("/system/ha", s.handleSetHA)
			r.Get("/system/ha/sensors", s.handleHASensors)
			r.Get("/system/oidc", s.handleGetOIDC)
			r.Put("/system/oidc", s.handleSetOIDC)
			r.Get("/system/detection", s.handleGetDetection)
			r.Put("/system/detection", s.handleSetDetection)
			r.Post("/system/detection/test", s.handleTestDetection)
			r.Get("/webhooks", s.handleListWebhooks)
			r.Post("/webhooks", s.handleCreateWebhook)
			r.Put("/webhooks/{id}", s.handleUpdateWebhook)
			r.Delete("/webhooks/{id}", s.handleDeleteWebhook)
			r.Post("/webhooks/{id}/test", s.handleTestWebhook)
			r.Get("/webhooks/{id}/deliveries", s.handleWebhookDeliveries)
			r.Get("/automations", s.handleListAutomations)
			r.Post("/automations", s.handleCreateAutomation)
			r.Put("/automations/{id}", s.handleUpdateAutomation)
			r.Delete("/automations/{id}", s.handleDeleteAutomation)
			r.Post("/automations/{id}/test", s.handleTestAutomation)
			r.Get("/system/audit", s.handleGetAudit)
			r.Get("/system/audit/verify", s.handleVerifyAudit)
		})
	})

	// Public share viewer (chrome-less, token-gated).
	r.Get("/s/{token}", s.handleSharePage)
	r.Get("/s/{token}/media", s.handleShareMedia)

	// Everything else: the SPA.
	r.NotFound(s.spaHandler())
	return r
}
