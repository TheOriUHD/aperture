// Command nvrd is the Aperture NVR daemon: it serves the web UI + API,
// orchestrates go2rtc, and (in later phases) supervises recording, motion
// detection, retention, and Home Assistant publishing.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/TheOriUHD/aperture/internal/api"
	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/motion"
	"github.com/TheOriUHD/aperture/internal/record"
	"github.com/TheOriUHD/aperture/internal/retention"
	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
	"github.com/TheOriUHD/aperture/internal/version"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log.Info("starting nvrd", "version", version.String())

	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "err", err)
		os.Exit(1)
	}

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	secureCookies := strings.HasPrefix(cfg.PublicURL, "https://")
	am := auth.NewManager(cfg.JWTSecret, cfg.StreamSecret, secureCookies)

	sm, err := stream.NewManager(cfg.Go2RTCURL, cfg.Go2RTCRTSP)
	if err != nil {
		log.Error("init stream manager", "err", err)
		os.Exit(1)
	}

	// Background services share a context cancelled on shutdown.
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	// Best-effort initial reconciliation of go2rtc streams with stored cameras.
	go func() {
		ctx, cancel := context.WithTimeout(bgCtx, 15*time.Second)
		defer cancel()
		cams, err := st.ListCameras()
		if err != nil {
			log.Error("initial stream sync: list cameras", "err", err)
			return
		}
		if err := sm.Sync(ctx, cams); err != nil {
			log.Warn("initial stream sync failed (go2rtc may not be up yet)", "err", err)
		}
	}()

	// Recorder (supervised ffmpeg + segment indexing) and retention janitor.
	rec := record.New(cfg, st, sm, log)
	go rec.Run(bgCtx)
	janitor := retention.New(cfg, st, log)
	go janitor.Run(bgCtx)

	// Motion detection (scene-change ticks -> events with thumbnails).
	det := motion.New(cfg, st, sm, log)
	go det.Run(bgCtx)

	// Keep a recent JPEG frame cached per camera for instant blurred placeholders.
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		warm := func() {
			ctx, cancel := context.WithTimeout(bgCtx, 10*time.Second)
			defer cancel()
			if cams, err := st.ListCameras(); err == nil {
				sm.WarmSnapshots(ctx, cams)
			}
		}
		warm()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-t.C:
				warm()
			}
		}
	}()

	srv := api.NewServer(cfg, st, am, sm, rec, det, log)
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("http listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown", "err", err)
	}
}
