package api

import (
	"context"
	"net/http"
	"time"

	"github.com/TheOriUHD/aperture/internal/site"
	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
	"github.com/TheOriUHD/aperture/internal/version"
)

// dayStartMs returns local midnight in the configured site timezone as unix ms,
// so "today" coverage and event metrics line up with the day the user sees in
// the timeline (which uses the browser's local day).
func (s *Server) dayStartMs() int64 {
	loc := time.Local
	if p := site.Load(s.st); p.Timezone != "" {
		if l, err := time.LoadLocation(p.Timezone); err == nil {
			loc = l
		}
	}
	now := time.Now().In(loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).UnixMilli()
}

// Why a camera is or isn't recording, so that a camera idle by design can't be
// mistaken for a broken one. Only recStateFailed is a problem.
const (
	recStateRecording    = "recording"    // footage is arriving
	recStateNotScheduled = "notScheduled" // outside its recording schedule
	recStateOff          = "off"          // disabled, or set to never record
	recStateFailed       = "failed"       // should be recording and isn't
)

func recordingState(c *store.Camera, due, footageArriving bool) string {
	switch {
	case !c.Enabled || c.RecordMode == store.ModeNever:
		return recStateOff
	case footageArriving:
		return recStateRecording
	case !due:
		return recStateNotScheduled
	default:
		return recStateFailed
	}
}

// recordingDue is Recorder.RecordingDue, which owns the schedule.
func (s *Server) recordingDue(c *store.Camera) bool {
	if s.rec == nil {
		return c.Enabled && c.RecordMode != store.ModeNever
	}
	return s.rec.RecordingDue(c)
}

// recordingNow is Recorder.ShouldRecord, which owns the schedule.
func (s *Server) recordingNow(c *store.Camera) bool {
	if s.rec == nil {
		return c.Enabled && c.RecordMode != store.ModeNever
	}
	return s.rec.ShouldRecord(c)
}

// handleSystemStatus aggregates a live health snapshot: per-camera recording and
// live-view status, go2rtc reachability, storage, and integration state. The
// two camera signals are deliberately independent — recording pulls straight
// from the camera, while live view depends on go2rtc — so a "recording OK, live
// down" split (exactly the recent go2rtc-stream-drop) is visible at a glance.
func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.buildStatus(ctx, seesDisabledCameras(r)))
}

// buildStatus assembles the health snapshot, shared by the status endpoint and
// the diagnostics bundle.
func (s *Server) buildStatus(ctx context.Context, includeDisabled bool) map[string]any {
	health := s.sm.Health(ctx)
	cams, _ := s.st.ListCameras()
	now := time.Now().UnixMilli()
	dayStart := s.dayStartMs()

	camStatus := make([]map[string]any, 0, len(cams))
	liveDown, recDown := 0, 0
	for _, c := range cams {
		if !c.Enabled && !includeDisabled {
			continue
		}
		_, registered := health.Registered[stream.StreamName(c.ID)]

		var newest int64
		if st, err := s.st.CameraStat(c.ID); err == nil {
			newest = st.NewestTS
		}
		recordingExpected := s.recordingDue(c)
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
			"recordingState":    recordingState(c, recordingExpected, recordingOk),
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

	recorded, _ := s.st.TotalBytes()
	allocation := s.st.GetSettingInt64("storage_cap_bytes")
	used := recorded + s.appFilesBytes()
	allocFree := int64(0)
	if allocation > 0 && allocation > used {
		allocFree = allocation - used
	}
	totalAllocated, _, totalFree := s.storageTotals()
	ha := s.ha.LoadSettings()

	return map[string]any{
		"version":       version.Version,
		"uptimeSec":     int64(time.Since(s.startedAt).Seconds()),
		"go2rtc":        map[string]any{"reachable": health.Reachable, "streams": len(health.Registered)},
		"liveDownCount": liveDown,
		"recDownCount":  recDown,
		// allocatedBytes/freeBytes stay LOCAL-only (frozen v1 meaning); the total*
		// fields span every enabled destination — see Server.storageTotals.
		"storage": map[string]any{
			"allocatedBytes":      allocation,
			"usedBytes":           used,
			"freeBytes":           allocFree,
			"totalAllocatedBytes": totalAllocated,
			"totalFreeBytes":      totalFree,
		},
		"homeAssistant": map[string]any{"enabled": ha.Enabled, "connected": s.ha.Connected()},
		"weather":       s.weather.Status(),
		"cameras":       camStatus,
	}
}
