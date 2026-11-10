// Command nvrd is the Aperture NVR daemon: it serves the web UI + API,
// orchestrates go2rtc, and (in later phases) supervises recording, motion
// detection, retention, and Home Assistant publishing.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/TheOriUHD/aperture/internal/ai"
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
	"github.com/TheOriUHD/aperture/internal/logbuf"
	"github.com/TheOriUHD/aperture/internal/motion"
	"github.com/TheOriUHD/aperture/internal/notify"
	"github.com/TheOriUHD/aperture/internal/oidc"
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
	// Closed explicitly, last, at the end of shutdown (below).

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

	secureCookies := strings.HasPrefix(cfg.PublicURL, "https://")
	am := auth.NewManager(cfg.JWTSecret, cfg.StreamSecret, cfg.CLIToken, secureCookies)

	sm, err := stream.NewManager(cfg.Go2RTCURL, cfg.Go2RTCRTSP)
	if err != nil {
		log.Error("init stream manager", "err", err)
		os.Exit(1)
	}

	// Background services share a context cancelled on shutdown. The ones in
	// `waited` are also waited for before nvrd exits (see the end of main).
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()
	waited := &services{ctx: bgCtx}

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
	// Waited for on shutdown: its Run returns once every ffmpeg has closed the
	// segment it was writing.
	rec := record.New(cfg, st, sm, storageRegistry, log)
	waited.run("recorder", rec.Run)
	janitor := retention.New(cfg, st, storageRegistry, log)
	waited.run("retention", janitor.Run) // a pass in flight finishes before the store closes

	// Motion detection (scene-change ticks -> events with thumbnails). Waited
	// for so its ffmpeg are reaped by nvrd, not SIGKILLed by systemd after it.
	det := motion.New(cfg, st, sm, log)
	waited.run("motion", det.Run)

	// Audio detection (loudness ticks -> "audio" events). Opt-in per camera.
	audioDet := audio.New(cfg, st, sm, log)
	waited.run("audio", audioDet.Run)

	// Home Assistant MQTT publisher (outbound auto-discovery + state).
	siteName := func() string {
		if n, err := st.GetSetting("site_name"); err == nil && n != "" {
			return n
		}
		return "Home NVR"
	}
	haPub := ha.New(cfg, st, sm, siteName, log)
	waited.run("home assistant", haPub.Run) // publishes "offline" as it returns

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
		uid, scopes, grants, extID, ok := st.ResolveAPIToken(bearer)
		if !ok {
			return nil
		}
		u, err := st.GetUserByID(uid)
		if err != nil || u.Disabled {
			return nil
		}
		return &auth.SessionClaims{UserID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role, Class: auth.ClassToken, Scopes: scopes, CameraGrants: grants, ExtID: extID}
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
	haPub.SetShouldRecord(rec.ShouldRecord)
	haPub.SetOnArmed(notifyEng.OnAlarmState) // alarm.state on HA-driven arms (Item 3)
	// Local automation engine: events → matching automations → actions (notify,
	// webhook, MQTT publish, set record mode).
	automateEng := automate.New(st, notifyEng, haPub, reconcile, cfg.PublicURL, log)

	// HomeKit bridge (Apple Home app) — a UI toggle backed by a setting. The env
	// var only SEEDS that setting on first boot, so existing env-configured
	// deployments keep working while the operator can now flip it from the app.
	hkSvc := newHomeKitService(bgCtx, cfg, st, log, notifyEng.OnAlarmState)

	// Event-gated object detection (opt-in; no-op when mode=off). Runs before
	// notify/automations so both can filter on the detected labels.
	detectEng := detect.New(st, sm, cfg.DataDir, log)
	// Aperture's own object recognition: runtimes and models are downloaded on
	// request into a folder beside the data dir (not inside it: the retention
	// janitor counts the data dir against the recordings budget), and run in the
	// separate apdetect worker.
	aiMgr := ai.NewManager(filepath.Join(filepath.Dir(cfg.DataDir), "ai"), helperPath("apdetect"), aiThreads(), log)
	liveSink := detectEng.EnableLocal(aiMgr, func(typ, camera string, data any) {
		notifyEng.PublishFeed(notify.FeedMessage{Type: typ, TS: time.Now().UnixMilli(), Camera: camera, Data: data})
	}, det.Reconcile)
	det.SetFrameSink(liveSink)
	// A detector starting on (or falling back from) a GPU changes how many frames
	// are worth decoding for it.
	aiMgr.SetOnWorker(det.Reconcile)
	waited.run("object recognition", detectEng.Run)
	det.SetOnEvent(func(ev *store.Event) {
		detectEng.EventOpened(ev)
		detectEng.Enrich(ev)
		notifyEng.OnEvent(ev)
		webhookDisp.OnEvent(ev)
		automateEng.OnEvent(ev)
		if b := hkSvc.Bridge(); b != nil {
			b.MotionPulse(ev.CameraID)
		}
	})
	// Change-feed domain emitters (Phase 3): event.closed, detection.created.
	det.SetOnEventClose(func(id string) {
		detectEng.EventClosed(id)
		notifyEng.OnEventClose(id)
	})
	detectEng.SetOnDetections(notifyEng.OnDetections)
	// Audio events flow through the same notify/automation fan-out (minus object
	// detection, which is video-only).
	audioDet.SetOnEvent(func(ev *store.Event) {
		notifyEng.OnEvent(ev)
		webhookDisp.OnEvent(ev)
		automateEng.OnEvent(ev)
	})
	audioDet.SetOnEventClose(notifyEng.OnEventClose) // audio event.closed (Item 3)
	// Package deliveries and pick-ups are events of their own (detect/packages.go).
	// Home Assistant presence: who and what each camera sees (person, people,
	// vehicle, animal, parked vehicle, package), with recognition looking at
	// the published cameras every second or so while HA is connected.
	haPub.SetPresence(func(id string) (ha.Presence, bool) {
		p, ok := detectEng.Presence(id)
		return ha.Presence{Person: p.Person, People: p.People, Vehicle: p.Vehicle, Animal: p.Animal,
			Parked: p.Parked, Package: p.Package, Packages: p.Packages}, ok
	}, detectEng.KeepPresence, detectEng.Moving)
	detectEng.SetEventSink(cfg.RecordingsDir, func(ev *store.Event) {
		notifyEng.OnEvent(ev)
		webhookDisp.OnEvent(ev)
		automateEng.OnEvent(ev)
	})

	// Environmental context: poll weather into a time-series for "past weather
	// at the playhead". No-ops until a site location is configured.
	weatherPoller := weather.New(st, log)
	waited.run("weather", weatherPoller.Run)

	// Watchdog: detect stale recordings (wedged producers) and self-heal.
	wd := watchdog.New(st, sm, rec, log)
	wd.SetOnCameraState(notifyEng.OnCameraState)
	waited.run("watchdog", wd.Run)

	// ONVIF Profile S provider (external VMS/NVR consumption) — same toggle model
	// as HomeKit above.
	onvifSvc := newONVIFService(bgCtx, cfg, st, sm, log)

	// Seed each setting from its env var the first time only, then start whatever
	// the settings say. A start failure is logged and leaves the service stopped —
	// the UI reports enabled-but-not-running rather than a false green light.
	seedInterop(st, "onvif.enabled", cfg.ONVIFEnabled)
	seedInterop(st, "homekit.enabled", cfg.HomeKitEnabled)
	if st.GetSettingBool("onvif.enabled") {
		if err := onvifSvc.SetEnabled(true); err != nil {
			log.Error("onvif: start failed", "err", err)
		}
	}
	if st.GetSettingBool("homekit.enabled") {
		if err := hkSvc.SetEnabled(true); err != nil {
			log.Error("homekit: start failed", "err", err)
		}
	}

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
	srv.SetInterop(onvifSvc, hkSvc) // ONVIF / HomeKit toggles reach the running services
	srv.StartRateLimitSweeper()
	httpSrv := srv.NewHTTPServer(cfg.HTTPAddr) // ends event streams on Shutdown

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
	began := time.Now()

	// 1. Extension children first, while the API they call is still up: SIGTERM,
	// SIGKILL after their grace window. StopAll only signals, so wait for them.
	extSup.StopAll()
	extCtx, extCancel := context.WithTimeout(context.Background(), extSup.StopGrace()+extStopSlack)
	if !extSup.Wait(extCtx) {
		log.Warn("shutdown: extensions still running after their stop window")
	}
	extCancel()
	extDone := time.Now()

	// 2. Background services. The recorder has every ffmpeg finish and close the
	// segment it is writing, so no footage is lost to the restart; the detectors
	// reap their ffmpeg; Home Assistant is told the NVR is going offline.
	bgCancel()
	if pending := waited.wait(bgStopWait); len(pending) > 0 {
		log.Warn("shutdown: background services did not stop in time", "pending", pending, "after", bgStopWait)
	}
	bgDone := time.Now()
	slowName, slowTook := waited.slowest(extDone)

	// 3. HTTP last, so the web app and live view keep working until the end and the
	// event streams end just before the next instance starts listening: their
	// EventSource reconnects straight to it.
	shutdownHTTP(httpSrv, httpStopWait, log)
	httpDone := time.Now()

	// 4. Storage last of all, once nothing can use it.
	storageRegistry.Close()
	if err := st.Close(); err != nil {
		log.Warn("shutdown: close store", "err", err)
	}
	ms := func(d time.Duration) time.Duration { return d.Round(time.Millisecond) }
	log.Info("shutdown complete", "took", ms(time.Since(began)),
		"extensions", ms(extDone.Sub(began)), "background", ms(bgDone.Sub(extDone)),
		"slowest", slowName, "slowestTook", ms(slowTook),
		"http", ms(httpDone.Sub(bgDone)), "storage", ms(time.Since(httpDone)))
}

// seedInterop writes an interop toggle's initial value from its env var the FIRST
// time only (the settings row is then the source of truth). This keeps a
// deployment that set APERTURE_ONVIF_ENABLED / APERTURE_HOMEKIT_ENABLED working
// unchanged, while letting the UI own the switch from then on.
func seedInterop(st *store.Store, key string, envDefault bool) {
	if _, err := st.GetSetting(key); err == nil {
		return // already chosen by the operator
	}
	_ = st.SetSettingBool(key, envDefault)
}

// helperPath finds a helper binary shipped with nvrd: on PATH (the unit adds
// /usr/lib/aperture/bin), next to nvrd, or in the vendored folder.
func helperPath(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if self, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(self), name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join("/usr/lib/aperture/bin", name)
}

// aiThreads is how many CPU threads one inference may use: half the CPUs this
// process may run on, between 1 and 4, leaving the rest to recording.
func aiThreads() int {
	n := runtime.NumCPU() / 2
	if q := cgroupCPUs(); q > 0 && q/2 < n {
		n = q / 2
	}
	return max(1, min(4, n))
}

// cgroupCPUs reads the service's CPU quota in whole CPUs (0 when unlimited),
// from its own cgroup (e.g. CPUQuota= in the systemd unit).
func cgroupCPUs() int {
	path := "/sys/fs/cgroup/cpu.max"
	if b, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if rel, ok := strings.CutPrefix(line, "0::"); ok {
				path = filepath.Join("/sys/fs/cgroup", rel, "cpu.max")
			}
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var quota, period int
	if n, _ := fmt.Sscanf(string(b), "%d %d", &quota, &period); n != 2 || period <= 0 {
		return 0
	}
	return quota / period
}
