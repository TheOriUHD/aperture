package api

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
)

// frameSem bounds concurrent ffmpeg frame extractions so a hover-storm can't
// saturate CPU and starve live recording.
var frameSem = make(chan struct{}, 2)

// handleFrame extracts (and caches) a single downscaled JPEG of recorded footage
// at ?t=<unix ms>, for hover-scrub timeline previews. The timestamp is quantized
// to 1s so a fast hover collapses to a handful of cache keys.
func (s *Server) handleFrame(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.allowedCamera(r, id) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	t, err := strconv.ParseInt(r.URL.Query().Get("t"), 10, 64)
	if err != nil || t <= 0 {
		writeError(w, http.StatusBadRequest, "invalid t")
		return
	}
	t = (t / 1000) * 1000

	cacheDir := filepath.Join(s.cfg.FrameCacheDir(), id)
	cachePath := filepath.Join(cacheDir, strconv.FormatInt(t, 10)+".jpg")
	if _, err := os.Stat(cachePath); err == nil {
		w.Header().Set("Cache-Control", "public, max-age=1800")
		w.Header().Set("Content-Type", "image/jpeg")
		http.ServeFile(w, r, cachePath)
		return
	}

	segs, err := s.st.ListSegments(id, t, t+1)
	if err != nil || len(segs) == 0 {
		writeError(w, http.StatusNotFound, "no footage at that time")
		return
	}
	seg := segs[0]
	for _, sg := range segs {
		if sg.StartTS <= t && t < sg.EndTS {
			seg = sg
			break
		}
	}
	if _, err := os.Stat(seg.Path); err != nil {
		writeError(w, http.StatusGone, "footage file missing")
		return
	}
	offset := float64(t-seg.StartTS) / 1000.0

	frameSem <- struct{}{}
	defer func() { <-frameSem }()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	jpeg, err := extractFrameAtOffset(ctx, seg.Path, offset)
	if err != nil || len(jpeg) == 0 {
		writeError(w, http.StatusInternalServerError, "could not extract frame")
		return
	}

	// Write via a unique temp file so two concurrent requests for the same frame
	// can't corrupt each other's cache write; rename is atomic (last writer wins,
	// both valid).
	_ = os.MkdirAll(cacheDir, 0o755)
	if f, e := os.CreateTemp(cacheDir, "frame-*.tmp"); e == nil {
		if _, e2 := f.Write(jpeg); e2 == nil {
			_ = f.Close()
			_ = os.Rename(f.Name(), cachePath)
		} else {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=1800")
	w.Header().Set("Content-Type", "image/jpeg")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jpeg)
}

// extractFrameAtOffset pulls one downscaled JPEG from a segment file. -ss before
// -i makes this a cheap input-seek; scale keeps previews small.
func extractFrameAtOffset(ctx context.Context, path string, offset float64) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-ss", strconv.FormatFloat(offset, 'f', 3, 64),
		"-i", path, "-frames:v", "1", "-vf", "scale=320:-2", "-q:v", "5", "-f", "mjpeg", "-")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %v: %s", err, errb.String())
	}
	return out.Bytes(), nil
}

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
	if !s.allowedCamera(r, id) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
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
	if !s.allowedCamera(r, id) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
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
	if !s.allowedCamera(r, id) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	segID := strings.TrimSuffix(chi.URLParam(r, "seg"), ".ts")
	seg, err := s.st.SegmentByID(segID)
	if err != nil || seg.CameraID != id {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	http.ServeFile(w, r, seg.Path)
}

// recordingsDir is the directory recordings currently live in: the operator
// override if set, otherwise the deploy default.
func (s *Server) recordingsDir() string {
	if o := s.st.RecordingsPathOverride(); o != "" {
		return o
	}
	return s.cfg.RecordingsDir
}

// handleStorage reports recorded bytes, per-camera usage, disk totals, the
// active path, and the global cap.
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
	total, free := diskUsage(s.recordingsDir())
	writeJSON(w, http.StatusOK, map[string]any{
		"recordedBytes":  recorded,
		"diskTotalBytes": total,
		"diskFreeBytes":  free,
		"cameras":        perCam,
		"path":           s.recordingsDir(),
		"defaultPath":    s.cfg.RecordingsDir,
		"capBytes":       s.st.GetSettingInt64("storage_cap_bytes"),
	})
}

// handleSetStorageConfig updates the global storage cap and recordings path.
// The path is validated as writable before it takes effect; changing it
// restarts the recorders so new footage lands in the new location.
func (s *Server) handleSetStorageConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CapBytes int64  `json:"capBytes"`
		Path     string `json:"path"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CapBytes < 0 {
		req.CapBytes = 0
	}

	newPath := strings.TrimSpace(req.Path)
	if newPath == s.cfg.RecordingsDir {
		newPath = "" // equal to default = no override
	}
	if newPath != "" {
		if err := checkWritableDir(newPath); err != nil {
			writeError(w, http.StatusBadRequest, "path not usable: "+err.Error())
			return
		}
	}

	oldPath := s.st.RecordingsPathOverride()
	_ = s.st.SetSetting("storage_cap_bytes", strconv.FormatInt(req.CapBytes, 10))
	_ = s.st.SetSetting("recordings_path", newPath)

	if newPath != oldPath {
		s.rec.RestartAll()
		s.logAudit(r, "storage.path", newPath, "recordings path changed")
	}
	s.logAudit(r, "storage.config", "", "cap="+strconv.FormatInt(req.CapBytes, 10))
	s.handleStorage(w, r)
}

// checkWritableDir ensures a directory exists and is writable by the daemon.
func checkWritableDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	probe := filepath.Join(path, ".aperture-write-test")
	f, err := os.Create(probe)
	if err != nil {
		return err
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return nil
}

func diskUsage(path string) (total, free int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize)
}
