package api

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
	"github.com/TheOriUHD/aperture/internal/version"
)

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// handleMetrics emits a Prometheus text-exposition snapshot, hand-rolled (no
// client dependency) from the same data the health dashboard uses.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()

	health := s.sm.Health(ctx)
	cams, _ := s.st.ListCameras()
	now := time.Now().UnixMilli()
	dayStart := time.Now().Truncate(24 * time.Hour).UnixMilli()
	total, free := diskUsage(s.cfg.RecordingsDir)
	recorded, _ := s.st.TotalBytes()
	events, _ := s.st.CountEventsSince(dayStart)

	var b strings.Builder
	decl := func(name, help, typ string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
	}
	gauge := func(name, help string, v any) {
		decl(name, help, "gauge")
		fmt.Fprintf(&b, "%s %v\n", name, v)
	}

	gauge("aperture_uptime_seconds", "Daemon uptime in seconds.", int64(time.Since(s.startedAt).Seconds()))
	gauge("aperture_go2rtc_reachable", "Whether the go2rtc API is reachable.", b2f(health.Reachable))
	gauge("aperture_go2rtc_streams", "Number of registered go2rtc streams.", len(health.Registered))
	if s.rec != nil {
		gauge("aperture_recorders_active", "Active per-camera recorder processes.", s.rec.ActiveCount())
	}
	gauge("aperture_disk_total_bytes", "Recordings filesystem total bytes.", total)
	gauge("aperture_disk_free_bytes", "Recordings filesystem free bytes.", free)
	gauge("aperture_recorded_bytes", "Total indexed recorded bytes.", recorded)
	gauge("aperture_events_today", "Motion events since local midnight.", events)

	decl("aperture_camera_recording_ok", "1 if the camera produced a segment within the grace window.", "gauge")
	for _, c := range cams {
		var newest int64
		if st, err := s.st.CameraStat(c.ID); err == nil {
			newest = st.NewestTS
		}
		ok := newest > 0 && now-newest < store.RecordingGraceMs
		fmt.Fprintf(&b, "aperture_camera_recording_ok{camera_id=%q,name=%q} %g\n", c.ID, c.Name, b2f(ok))
	}
	decl("aperture_camera_live_ok", "1 if the camera's go2rtc stream is registered.", "gauge")
	for _, c := range cams {
		_, reg := health.Registered[stream.StreamName(c.ID)]
		fmt.Fprintf(&b, "aperture_camera_live_ok{camera_id=%q,name=%q} %g\n", c.ID, c.Name, b2f(health.Reachable && reg))
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

// handleDiagnostics streams a tar.gz support bundle: recent logs, the current
// status snapshot, sanitized config (no secrets), and the version.
func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	statusJSON, _ := json.MarshalIndent(s.buildStatus(ctx), "", "  ")
	cfgJSON, _ := json.MarshalIndent(map[string]any{
		"httpAddr":      s.cfg.HTTPAddr,
		"dataDir":       s.cfg.DataDir,
		"recordingsDir": s.cfg.RecordingsDir,
		"publicURL":     s.cfg.PublicURL,
		"go2rtcURL":     s.cfg.Go2RTCURL,
		"go2rtcRTSP":    s.cfg.Go2RTCRTSP,
		"note":          "secrets (JWT, stream, CLI token, passwords) intentionally omitted",
	}, "", "  ")

	var logs string
	if s.logs != nil {
		logs = strings.Join(s.logs.LastN(500), "\n")
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=aperture-diagnostics-%d.tar.gz", time.Now().Unix()))

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	add := func(name string, data []byte) {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), ModTime: time.Now()})
		_, _ = tw.Write(data)
	}
	add("status.json", statusJSON)
	add("config.json", cfgJSON)
	add("logs.txt", []byte(logs))
	add("version.txt", []byte(version.Version+" "+version.Commit+"\n"))
	_ = tw.Close()
	_ = gz.Close()
}
