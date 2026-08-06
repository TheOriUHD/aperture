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
	"github.com/TheOriUHD/aperture/internal/audio"
	"github.com/TheOriUHD/aperture/internal/audit"
	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/automate"
	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/detect"
	"github.com/TheOriUHD/aperture/internal/export"
	"github.com/TheOriUHD/aperture/internal/ext"
	"github.com/TheOriUHD/aperture/internal/ha"
	"github.com/TheOriUHD/aperture/internal/homekit"
	"github.com/TheOriUHD/aperture/internal/logbuf"
	"github.com/TheOriUHD/aperture/internal/motion"
	"github.com/TheOriUHD/aperture/internal/notify"
	"github.com/TheOriUHD/aperture/internal/oidc"
	"github.com/TheOriUHD/aperture/internal/onvif"
	"github.com/TheOriUHD/aperture/internal/record"
	"github.com/TheOriUHD/aperture/internal/retention"
	"github.com/TheOriUHD/aperture/internal/secret"
	"github.com/TheOriUHD/aperture/internal/storage"
	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
	"github.com/TheOriUHD/aperture/internal/version"
	"github.com/TheOriUHD/aperture/internal/watchdog"
	"github.com/TheOriUHD/aperture/internal/weather"
	"github.com/TheOriUHD/aperture/internal/webhook"
)

func main() {
	// If this process is a re-exec to launch a backend extension
	// (nvrd __ext-spawn …), apply its rlimits + OOM bias and exec Deno now — it
	// must never fall through into the normal daemon boot.
	ext.RunSpawnTrampoline()

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

	// Storage-credential cipher + backend registry for network storage locations.
	// The cipher may be nil only if no key could be derived (never, in practice) —
	// handlers degrade gracefully (network credentials disabled) when it is.
	storageCipher, cerr := secret.New(cfg.EncryptionKey)
	if cerr != nil {
		log.Warn("storage: encryption key unavailable; network storage credentials disabled", "err", cerr)
	}
	storageRegistry := storage.NewRegistry(func(locationID string) (*storage.Spec, error) {
		if locationID == store.DefaultLocationID {
			root := cfg.RecordingsDir
			if o := st.RecordingsPathOverride(); o != "" {
				root = o
			}
			return &storage.Spec{Type: storage.TypeLocal, Config: map[string]any{"path": root}}, nil
		}
		loc, err := st.GetStorageLocation(locationID)
		if err != nil {
			return nil, err
		}
		sec := ""
		if storageCipher != nil && loc.HasCredential {
			if enc, e := st.LocationSecret(locationID); e == nil {
				if dec, e2 := storageCipher.Decrypt(enc); e2 == nil {
					sec = dec
				}
			}
		}
		return &storage.Spec{Type: loc.Type, Config: loc.Config, Secret: sec}, nil
	})
	defer storageRegistry.Close()

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
	rec := record.New(cfg, st, sm, storageRegistry, log)
	go rec.Run(bgCtx)
	janitor := retention.New(cfg, st, storageRegistry, log)
	go janitor.Run(bgCtx)

	// Motion detection (scene-change ticks -> events with thumbnails).
	det := motion.New(cfg, st, sm, log)
	go det.Run(bgCtx)

	// Audio detection (loudness ticks -> "audio" events). Opt-in per camera.
	audioDet := audio.New(cfg, st, sm, log)
	go audioDet.Run(bgCtx)

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
	exporter := export.New(st, storageRegistry, filepath.Join(cfg.DataDir, "exports"), log)
	exporter.Start(bgCtx)
	notifyEng := notify.New(cfg, st, log)
	exporter.SetOnProgress(notifyEng.OnExportProgress) // change feed export.progress (3.2)
	// Outbound event webhooks (open-platform integration firehose).
	webhookDisp := webhook.New(st, cfg.PublicURL, log)
	// API tokens authenticate as their owning user (inherit role + camera ACLs).
	am.SetAPITokenResolver(func(bearer string) *auth.SessionClaims {
		if !strings.HasPrefix(bearer, store.APITokenPrefix) {
			return nil
		}
		uid, scopes, grants, ok := st.ResolveAPIToken(bearer)
		if !ok {
			return nil
		}
		u, err := st.GetUserByID(uid)
		if err != nil || u.Disabled {
			return nil
		}
		return &auth.SessionClaims{UserID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role, Class: auth.ClassToken, Scopes: scopes, CameraGrants: grants}
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
	haPub.SetOnArmed(notifyEng.OnAlarmState) // alarm.state on HA-driven arms (Item 3)
	// Local automation engine: events → matching automations → actions (notify,
	// webhook, MQTT publish, set record mode).
	automateEng := automate.New(st, notifyEng, haPub, reconcile, cfg.PublicURL, log)

	// HomeKit bridge (Apple Home app), off unless APERTURE_HOMEKIT_ENABLED=true.
	var hkBridge *homekit.Bridge
	if cfg.HomeKitEnabled {
		if b, err := homekit.New(cfg, st, log); err == nil {
			hkBridge = b
			hkBridge.SetOnArmed(notifyEng.OnAlarmState) // alarm.state on HomeKit arms (Item 3)
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
			hkBridge.MotionPulse(ev.CameraID)
		}
	})
	// Change-feed domain emitters (Phase 3): event.closed, detection.created.
	det.SetOnEventClose(notifyEng.OnEventClose)
	detectEng.SetOnDetections(notifyEng.OnDetections)
	// Audio events flow through the same notify/automation fan-out (minus object
	// detection, which is video-only).
	audioDet.SetOnEvent(func(ev *store.Event) {
		notifyEng.OnEvent(ev)
		webhookDisp.OnEvent(ev)
		automateEng.OnEvent(ev)
	})
	audioDet.SetOnEventClose(notifyEng.OnEventClose) // audio event.closed (Item 3)

	// Environmental context: poll weather into a time-series for "past weather
	// at the playhead". No-ops until a site location is configured.
	weatherPoller := weather.New(st, log)
	go weatherPoller.Run(bgCtx)

	// Watchdog: detect stale recordings (wedged producers) and self-heal.
	wd := watchdog.New(st, sm, log)
	wd.SetOnCameraState(notifyEng.OnCameraState)
	go wd.Run(bgCtx)

	// ONVIF Profile S provider (external VMS/NVR consumption). No-op unless
	// APERTURE_ONVIF_ENABLED=true.
	onvifSrv := onvif.New(cfg, st, log)
	go onvifSrv.Run(bgCtx)

	// Backend-extension supervisor (T1.3). Constructed here so it can be drained on
	// shutdown; the install/manager handlers drive it in T1.5.
	denoPath := os.Getenv("APERTURE_DENO_PATH")
	if denoPath == "" {
		denoPath = "/usr/local/bin/deno"
	}
	extSup := ext.NewSupervisor(denoPath, log)

	srv := api.NewServer(cfg, st, am, sm, rec, det, detectEng, webhookDisp, automateEng, haPub, oidcAuth, auditLog, exporter, notifyEng, weatherPoller, wd, logRing, log)
	sdkModule := os.Getenv("APERTURE_EXT_SDK")
	if sdkModule == "" {
		sdkModule = "/usr/local/share/aperture-sdk/mod.ts"
	}
	apiBase := os.Getenv("APERTURE_URL")
	if apiBase == "" {
		apiBase = "http://localhost:8080"
	}
	srv.SetExtensions(extSup, denoPath, sdkModule, apiBase)
	srv.StartEnabledExtensions() // (re)start enabled backend extensions across the restart
	srv.SetStorage(storageRegistry, storageCipher)
	srv.SetReconcile(janitor.EnforceNow)
	srv.StartRateLimitSweeper()
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second, // slowloris guard on headers
		IdleTimeout:       120 * time.Second,
		// ReadTimeout and WriteTimeout are deliberately left 0: a write deadline
		// would kill SSE and long media exports, a read deadline slow large
		// uploads. Body size is bounded by limitBody (MaxBytesReader) instead.
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

	// Drain extension children first (SIGTERM → grace window) so they stop cleanly
	// before the background context cancels the recorder and the rest.
	extSup.StopAll()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown", "err", err)
	}
}
