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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/TheOriUHD/aperture/internal/api"
	"github.com/TheOriUHD/aperture/internal/audit"
	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/automate"
	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/detect"
	"github.com/TheOriUHD/aperture/internal/export"
	"github.com/TheOriUHD/aperture/internal/ha"
	"github.com/TheOriUHD/aperture/internal/homekit"
	"github.com/TheOriUHD/aperture/internal/logbuf"
	"github.com/TheOriUHD/aperture/internal/motion"
	"github.com/TheOriUHD/aperture/internal/notify"
	"github.com/TheOriUHD/aperture/internal/oidc"
	"github.com/TheOriUHD/aperture/internal/onvif"
	"github.com/TheOriUHD/aperture/internal/record"
	"github.com/TheOriUHD/aperture/internal/retention"
	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
	"github.com/TheOriUHD/aperture/internal/version"
	"github.com/TheOriUHD/aperture/internal/watchdog"
	"github.com/TheOriUHD/aperture/internal/weather"
	"github.com/TheOriUHD/aperture/internal/webhook"
)

func main() {
	logRing := logbuf.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}), 500)
	log := slog.New(logRing)
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
	am := auth.NewManager(cfg.JWTSecret, cfg.StreamSecret, cfg.CLIToken, secureCookies)

	sm, err := stream.NewManager(cfg.Go2RTCURL, cfg.Go2RTCRTSP)
	if err != nil {
		log.Error("init stream manager", "err", err)
		os.Exit(1)
	}

	// Background services share a context cancelled on shutdown.
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	// Reconcile go2rtc streams with stored cameras at startup and then on a
	// fixed interval. go2rtc keeps dynamically-added streams only in memory, so
	// a go2rtc restart silently drops them; periodic reconciliation makes live
	// view self-heal instead of staying dark until nvrd is restarted.
	go func() {
		reconcile := func() {
			ctx, cancel := context.WithTimeout(bgCtx, 15*time.Second)
			defer cancel()
			cams, err := st.ListCameras()
			if err != nil {
				log.Error("stream sync: list cameras", "err", err)
				return
			}
			n, err := sm.Sync(ctx, cams)
			if err != nil {
				log.Warn("stream sync failed (will retry)", "err", err)
				return
			}
			if n > 0 {
				log.Info("stream sync: registered camera streams in go2rtc", "count", n)
			}
		}
		reconcile()
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-t.C:
				reconcile()
			}
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

	// Home Assistant MQTT publisher (outbound auto-discovery + state).
	siteName := func() string {
		if n, err := st.GetSetting("site_name"); err == nil && n != "" {
			return n
		}
		return "Home NVR"
	}
	haPub := ha.New(cfg, st, sm, siteName, log)
	go haPub.Run(bgCtx)

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

	// Prune stale hover-scrub frame-cache files.
	go func() {
		t := time.NewTicker(30 * time.Minute)
		defer t.Stop()
		prune := func() {
			cutoff := time.Now().Add(-2 * time.Hour)
			_ = filepath.Walk(cfg.FrameCacheDir(), func(p string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				if info.ModTime().Before(cutoff) {
					_ = os.Remove(p)
				}
				return nil
			})
		}
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-t.C:
				prune()
			}
		}
	}()

	oidcAuth := oidc.New(cfg, st)
	auditLog := audit.New(st)
	exporter := export.New(st, filepath.Join(cfg.DataDir, "exports"), log)
	exporter.Start(bgCtx)
	notifyEng := notify.New(cfg, st, log)
	// Outbound event webhooks (open-platform integration firehose).
	webhookDisp := webhook.New(st, cfg.PublicURL, log)
	// API tokens authenticate as their owning user (inherit role + camera ACLs).
	am.SetAPITokenResolver(func(bearer string) *auth.SessionClaims {
		if !strings.HasPrefix(bearer, store.APITokenPrefix) {
			return nil
		}
		uid, ok := st.ResolveAPIToken(bearer)
		if !ok {
			return nil
		}
		u, err := st.GetUserByID(uid)
		if err != nil || u.Disabled {
			return nil
		}
		return &auth.SessionClaims{UserID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role}
	})
	// Applying a record-mode change (from an automation or HA command) means
	// re-syncing both the recorder and the motion detector immediately.
	reconcile := func() {
		rec.Reconcile()
		det.Reconcile()
	}
	// Home Assistant inbound control (record-mode Select / Arm switch) applies
	// record-mode changes through the same reconcile path.
	haPub.SetReconcile(reconcile)
	// Local automation engine: events → matching automations → actions (notify,
	// webhook, MQTT publish, set record mode).
	automateEng := automate.New(st, notifyEng, haPub, reconcile, cfg.PublicURL, log)

	// HomeKit bridge (Apple Home app), off unless APERTURE_HOMEKIT_ENABLED=true.
	var hkBridge *homekit.Bridge
	if cfg.HomeKitEnabled {
		if b, err := homekit.New(cfg, st, log); err == nil {
			hkBridge = b
			go hkBridge.Run(bgCtx)
		} else {
			log.Error("homekit: init failed", "err", err)
		}
	}

	// Event-gated object detection (opt-in; no-op when mode=off). Runs before
	// notify/automations so both can filter on the detected labels.
	detectEng := detect.New(st, sm, cfg.DataDir, log)
	det.SetOnEvent(func(ev *store.Event) {
		detectEng.Enrich(ev)
		notifyEng.OnEvent(ev)
		webhookDisp.OnEvent(ev)
		automateEng.OnEvent(ev)
		if hkBridge != nil {
			hkBridge.SetMotion(ev.CameraID, true)
			cam := ev.CameraID
			time.AfterFunc(20*time.Second, func() { hkBridge.SetMotion(cam, false) })
		}
	})

	// Environmental context: poll weather into a time-series for "past weather
	// at the playhead". No-ops until a site location is configured.
	weatherPoller := weather.New(st, log)
	go weatherPoller.Run(bgCtx)

	// Watchdog: detect stale recordings (wedged producers) and self-heal.
	wd := watchdog.New(st, sm, log)
	go wd.Run(bgCtx)

	// ONVIF Profile S provider (external VMS/NVR consumption). No-op unless
	// APERTURE_ONVIF_ENABLED=true.
	onvifSrv := onvif.New(cfg, st, log)
	go onvifSrv.Run(bgCtx)

	srv := api.NewServer(cfg, st, am, sm, rec, det, detectEng, webhookDisp, automateEng, haPub, oidcAuth, auditLog, exporter, notifyEng, weatherPoller, wd, logRing, log)
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
