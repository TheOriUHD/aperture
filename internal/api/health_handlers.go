package api

import (
	"context"
	"net/http"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
	"github.com/TheOriUHD/aperture/internal/version"
)

// recordingGraceMs is how stale the newest segment may be before a camera in a
// recording mode is considered to have stopped delivering.
const recordingGraceMs = 90_000

// handleSystemStatus aggregates a live health snapshot: per-camera recording and
// live-view status, go2rtc reachability, storage, and integration state. The
// two camera signals are deliberately independent — recording pulls straight
// from the camera, while live view depends on go2rtc — so a "recording OK, live
// down" split (exactly the recent go2rtc-stream-drop) is visible at a glance.
func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	health := s.sm.Health(ctx)
	cams, _ := s.st.ListCameras()
	now := time.Now().UnixMilli()

	camStatus := make([]map[string]any, 0, len(cams))
	liveDown, recDown := 0, 0
	for _, c := range cams {
		_, registered := health.Registered[stream.StreamName(c.ID)]

		var newest int64
		if st, err := s.st.CameraStat(c.ID); err == nil {
			newest = st.NewestTS
		}
		recordingExpected := c.Enabled && c.RecordMode != store.ModeNever
		recordingOk := newest > 0 && now-newest < recordingGraceMs
		liveOk := health.Reachable && registered

		if c.Enabled && !liveOk {
			liveDown++
		}
		if recordingExpected && !recordingOk {
			recDown++
		}
		camStatus = append(camStatus, map[string]any{
			"cameraId":          c.ID,
			"name":              c.Name,
			"enabled":           c.Enabled,
			"recordMode":        c.RecordMode,
			"recordingExpected": recordingExpected,
			"recordingOk":       recordingOk,
			"lastSegmentTs":     newest,
			"liveOk":            liveOk,
		})
	}

	total, free := diskUsage(s.cfg.RecordingsDir)
	recorded, _ := s.st.TotalBytes()
	ha := s.ha.LoadSettings()

	writeJSON(w, http.StatusOK, map[string]any{
		"version":       version.Version,
		"uptimeSec":     int64(time.Since(s.startedAt).Seconds()),
		"go2rtc":        map[string]any{"reachable": health.Reachable, "streams": len(health.Registered)},
		"liveDownCount": liveDown,
		"recDownCount":  recDown,
		"storage":       map[string]any{"totalBytes": total, "freeBytes": free, "recordedBytes": recorded},
		"homeAssistant": map[string]any{"enabled": ha.Enabled, "connected": s.ha.Connected()},
		"weather":       s.weather.Status(),
		"cameras":       camStatus,
	})
}
