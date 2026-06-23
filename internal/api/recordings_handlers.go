package api

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
)

// parseRange reads from/to (unix ms) query params, defaulting to the last 24h.
func parseRange(r *http.Request) (from, to int64) {
	now := time.Now().UnixMilli()
	from, to = now-24*3600*1000, now
	if v, err := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64); err == nil {
		from = v
	}
	if v, err := strconv.ParseInt(r.URL.Query().Get("to"), 10, 64); err == nil {
		to = v
	}
	return from, to
}

// handleSegmentsList returns the recorded segments overlapping a time range,
// used to draw the timeline.
func (s *Server) handleSegmentsList(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	from, to := parseRange(r)
	segs, err := s.st.ListSegments(id, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(segs))
	for _, seg := range segs {
		out = append(out, map[string]any{
			"id":         seg.ID,
			"startTs":    seg.StartTS,
			"endTs":      seg.EndTS,
			"durationMs": seg.DurationMS,
			"hasMotion":  seg.HasMotion,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleHLS generates a VOD HLS playlist from the stored segments in a range.
// Time gaps (e.g. an ffmpeg restart) become EXT-X-DISCONTINUITY markers.
func (s *Server) handleHLS(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	from, to := parseRange(r)
	segs, err := s.st.ListSegments(id, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	maxDur := 1.0
	for _, seg := range segs {
		if d := float64(seg.DurationMS) / 1000; d > maxDur {
			maxDur = d
		}
	}

	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-PLAYLIST-TYPE:VOD\n")
	b.WriteString("#EXT-X-TARGETDURATION:" + strconv.Itoa(int(math.Ceil(maxDur))) + "\n")
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	var prevEnd int64
	for i, seg := range segs {
		if i > 0 && seg.StartTS-prevEnd > 1000 {
			b.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		b.WriteString("#EXTINF:" + strconv.FormatFloat(float64(seg.DurationMS)/1000, 'f', 3, 64) + ",\n")
		b.WriteString("seg/" + seg.ID + ".ts\n")
		prevEnd = seg.EndTS
	}
	b.WriteString("#EXT-X-ENDLIST\n")

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(b.String()))
}

// handleSegmentFile serves a single recorded segment (with range support for
// seeking).
func (s *Server) handleSegmentFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	segID := strings.TrimSuffix(chi.URLParam(r, "seg"), ".ts")
	seg, err := s.st.SegmentByID(segID)
	if err != nil || seg.CameraID != id {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	http.ServeFile(w, r, seg.Path)
}

// handleStorage reports recorded bytes, per-camera usage, and disk totals.
func (s *Server) handleStorage(w http.ResponseWriter, r *http.Request) {
	recorded, _ := s.st.TotalBytes()
	cams, _ := s.st.ListCameras()
	perCam := make([]any, 0, len(cams))
	for _, c := range cams {
		if stat, err := s.st.CameraStat(c.ID); err == nil {
			perCam = append(perCam, map[string]any{
				"cameraId": c.ID,
				"name":     c.Name,
				"bytes":    stat.Bytes,
				"count":    stat.Count,
				"oldestTs": stat.OldestTS,
				"newestTs": stat.NewestTS,
			})
		}
	}
	total, free := diskUsage(s.cfg.RecordingsDir)
	writeJSON(w, http.StatusOK, map[string]any{
		"recordedBytes": recorded,
		"diskTotalBytes": total,
		"diskFreeBytes":  free,
		"cameras":        perCam,
	})
}

func diskUsage(path string) (total, free int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize)
}
