package api

import (
	"context"
	"net/http"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
	"github.com/TheOriUHD/aperture/internal/version"
)

// handleSystemStatus aggregates a live health snapshot: per-camera recording and
// live-view status, go2rtc reachability, storage, and integration state. The
// two camera signals are deliberately independent — recording pulls straight
// from the camera, while live view depends on go2rtc — so a "recording OK, live
// down" split (exactly the recent go2rtc-stream-drop) is visible at a glance.
func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.buildStatus(ctx))
}

// buildStatus assembles the health snapshot, shared by the status endpoint and
// the diagnostics bundle.
func (s *Server) buildStatus(ctx context.Context) map[string]any {
	health := s.sm.Health(ctx)
	cams, _ := s.st.ListCameras()
	now := time.Now().UnixMilli()
	dayStart := time.Now().Truncate(24 * time.Hour).UnixMilli()

	camStatus := make([]map[string]any, 0, len(cams))
	liveDown, recDown := 0, 0
	for _, c := range cams {
		_, registered := health.Registered[stream.StreamName(c.ID)]

		var newest int64
		if st, err := s.st.CameraStat(c.ID); err == nil {
			newest = st.NewestTS
		}
		recordingExpected := c.Enabled && c.RecordMode != store.ModeNever
		recordingOk := newest > 0 && now-newest < store.RecordingGraceMs
		liveOk := health.Reachable && registered

		if c.Enabled && !liveOk {
			liveDown++
		}
		if recordingExpected && !recordingOk {
			recDown++
		}

		cam := map[string]any{
			"cameraId":          c.ID,
			"name":              c.Name,
			"enabled":           c.Enabled,
			"recordMode":        c.RecordMode,
			"recordingExpected": recordingExpected,
			"recordingOk":       recordingOk,
			"lastSegmentTs":     newest,
			"liveOk":            liveOk,
		}
		// Watchdog self-heal stats.
		if s.watchdog != nil {
			seen, restarts, stale := s.watchdog.Stats(c.ID)
			cam["watchdogStale"] = stale
			cam["restartsToday"] = restarts
			if seen > 0 {
				cam["watchdogLastSeen"] = seen
			}
		}
		// Today's coverage % for continuous cameras (recording-gap signal).
		if c.Enabled && c.RecordMode == store.ModeContinuous && now > dayStart {
			if covered, err := s.st.DayCoverage(c.ID, dayStart, now); err == nil {
				cam["coveragePercent"] = int(float64(covered) / float64(now-dayStart) * 100)
			}
		}
		camStatus = append(camStatus, cam)
	}

	total, free := diskUsage(s.cfg.RecordingsDir)
	recorded, _ := s.st.TotalBytes()
	ha := s.ha.LoadSettings()

	return map[string]any{
		"version":       version.Version,
		"uptimeSec":     int64(time.Since(s.startedAt).Seconds()),
		"go2rtc":        map[string]any{"reachable": health.Reachable, "streams": len(health.Registered)},
		"liveDownCount": liveDown,
		"recDownCount":  recDown,
		"storage":       map[string]any{"totalBytes": total, "freeBytes": free, "recordedBytes": recorded},
		"homeAssistant": map[string]any{"enabled": ha.Enabled, "connected": s.ha.Connected()},
		"weather":       s.weather.Status(),
		"cameras":       camStatus,
	}
}
