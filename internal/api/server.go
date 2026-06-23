package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/motion"
	"github.com/TheOriUHD/aperture/internal/record"
	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
)

const sessionTTL = 7 * 24 * time.Hour

// Server wires the HTTP API and the embedded web UI.
type Server struct {
	cfg    *config.Config
	st     *store.Store
	am     *auth.Manager
	sm     *stream.Manager
	rec    *record.Recorder
	det    *motion.Detector
	log    *slog.Logger
	router http.Handler
}

// NewServer builds a Server and its routes.
func NewServer(cfg *config.Config, st *store.Store, am *auth.Manager, sm *stream.Manager, rec *record.Recorder, det *motion.Detector, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, st: st, am: am, sm: sm, rec: rec, det: det, log: log}
	s.router = s.routes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.am.Context)

	r.Route("/api", func(r chi.Router) {
		// Public.
		r.Get("/system/health", s.handleHealth)
		r.Get("/system/version", s.handleVersion)
		r.Get("/setup/status", s.handleSetupStatus)
		r.Post("/setup", s.handleSetup)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)

		// Live stream WebSocket (authorized via short-lived stream token in query).
		r.Get("/cameras/{id}/ws", s.handleStreamWS)

		// Authenticated.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Get("/auth/me", s.handleMe)
			r.Get("/cameras", s.handleListCameras)
			r.Get("/cameras/{id}", s.handleGetCamera)
			r.Get("/cameras/{id}/stream-token", s.handleStreamToken)
			r.Get("/cameras/{id}/snapshot.jpg", s.handleSnapshot)
			r.Get("/cameras/{id}/segments", s.handleSegmentsList)
			r.Get("/cameras/{id}/events", s.handleCameraEvents)
			r.Get("/cameras/{id}/hls", s.handleHLS)
			r.Get("/cameras/{id}/seg/{seg}", s.handleSegmentFile)
			r.Get("/events", s.handleEventsAll)
			r.Get("/events/{id}/thumb.jpg", s.handleEventThumb)
			r.Get("/cameras/{id}/schedule", s.handleGetSchedule)
			r.Get("/system/storage", s.handleStorage)
			r.Get("/system/settings", s.handleGetSettings)
		})

		// Admin-only.
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Use(auth.RequireAdmin)
			r.Post("/cameras", s.handleCreateCamera)
			r.Put("/cameras/{id}", s.handleUpdateCamera)
			r.Delete("/cameras/{id}", s.handleDeleteCamera)
			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)
			r.Put("/cameras/{id}/schedule", s.handleSetSchedule)
			r.Put("/system/settings", s.handleSetSettings)
			r.Post("/system/purge", s.handlePurge)
		})
	})

	// Everything else: the SPA.
	r.NotFound(s.spaHandler())
	return r
}
