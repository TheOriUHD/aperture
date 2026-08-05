package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/reserve"
	"github.com/TheOriUHD/aperture/internal/store"
)

// removeSegmentFile deletes a segment's bytes from wherever they live: local
// segments store an absolute path (os.Remove); network segments store a
// backend-relative path (routed through the storage registry).
func (s *Server) removeSegmentFile(seg *store.Segment) {
	if seg.Path == "" {
		return
	}
	if filepath.IsAbs(seg.Path) {
		if err := os.Remove(seg.Path); err != nil && !os.IsNotExist(err) {
			s.log.Warn("remove segment file", "path", seg.Path, "err", err)
		}
		return
	}
	if s.registry == nil {
		return
	}
	if b, err := s.registry.For(seg.LocationID); err == nil {
		_ = b.Remove(seg.Path)
	}
}

// purgeCameraFiles removes every on-disk artifact owned by a camera — recorded
// segment media (local or remote, routed through removeSegmentFile), event
// thumbnails/snapshots, and the per-camera working directories. Deleting a
// camera used to cascade only the DB rows, orphaning its footage (gigabytes)
// on disk forever. The rows themselves are left to the camera-row cascade in
// DeleteCamera; this only reclaims the bytes. Best-effort throughout: a missing
// file or dir is not an error.
func (s *Server) purgeCameraFiles(cameraID string) {
	// [0, ~4.6e18 ms] spans every possible segment/event timestamp. SegmentsInRange
	// is unfiltered by `locked`, so this reclaims all media (locked is inert today,
	// but a camera delete must not leave anything behind regardless).
	const allTime int64 = 1 << 62
	if segs, err := s.st.SegmentsInRange(cameraID, 0, allTime); err == nil {
		for _, seg := range segs {
			s.removeSegmentFile(seg)
		}
	}
	if evs, err := s.st.EventsInRange(cameraID, 0, allTime); err == nil {
		for _, e := range evs {
			if e.ThumbPath != "" {
				_ = os.Remove(e.ThumbPath)
			}
			if e.SnapshotPath != "" {
				_ = os.Remove(e.SnapshotPath)
			}
		}
	}
	// Sweep the per-camera working directories for anything ffmpeg wrote but had
	// not yet indexed (reindex-gap stragglers, the in-flight segment).
	for _, dir := range []string{
		filepath.Join(s.cfg.RecordingsDir, cameraID),
		filepath.Join(s.cfg.SpoolDir(), cameraID),
		filepath.Join(s.cfg.FrameCacheDir(), cameraID),
	} {
		_ = os.RemoveAll(dir)
	}
}

// localizeSegment returns a local filesystem path to a segment's media plus a
// release func to call when done. Local segments return their own path (release
// is a no-op); network segments are downloaded into a temp file under the remote
// cache dir, which release removes.
func (s *Server) localizeSegment(seg *store.Segment) (path string, release func(), err error) {
	noop := func() {}
	if seg.Path == "" {
		return "", noop, os.ErrNotExist
	}
	if filepath.IsAbs(seg.Path) {
		if _, err := os.Stat(seg.Path); err != nil {
			return "", noop, err
		}
		return seg.Path, noop, nil
	}
	if s.registry == nil {
		return "", noop, os.ErrNotExist
	}
	backend, err := s.registry.For(seg.LocationID)
	if err != nil {
		return "", noop, err
	}
	rc, _, err := backend.Open(seg.Path)
	if err != nil {
		return "", noop, err
	}
	defer rc.Close()
	tmp, err := os.CreateTemp(s.cfg.RemoteCacheDir(), "seg-*.ts")
	if err != nil {
		return "", noop, err
	}
	if _, err := io.Copy(tmp, rc); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", noop, err
	}
	_ = tmp.Close()
	name := tmp.Name()
	return name, func() { _ = os.Remove(name) }, nil
}

// frameSem bounds concurrent ffmpeg frame extractions so a hover-storm can't
// saturate CPU and starve live recording.
var frameSem = make(chan struct{}, 2)

// handleFrame extracts (and caches) a single downscaled JPEG of recorded footage
// at ?t=<unix ms>, for hover-scrub timeline previews. The timestamp is quantized
// to 1s so a fast hover collapses to a handful of cache keys.
func (s *Server) handleFrame(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
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
	// Resolve a local file for ffmpeg — remote segments are pulled into a temp
	// file first (the extracted JPEG is then frame-cached, so this download happens
	// at most once per timestamp).
	localPath, release, err := s.localizeSegment(seg)
	if err != nil {
		writeError(w, http.StatusGone, "footage file missing")
		return
	}
	defer release()
	offset := float64(t-seg.StartTS) / 1000.0

	frameSem <- struct{}{}
	defer func() { <-frameSem }()
	// The remote download (if any) already happened in localizeSegment, above and
	// outside this deadline; this only bounds the local ffmpeg frame extract, so
	// keep it tight to cap CPU on slow/corrupt footage.
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	jpeg, err := extractFrameAtOffset(ctx, localPath, offset)
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

// handleDeleteRecordings removes a camera's recorded footage (and overlapping
// events) in a time range — the Playback "Remove" action. Admin only; locked
// (saved) segments are skipped so an accidental sweep can't wipe kept clips.
func (s *Server) handleDeleteRecordings(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c := auth.FromContext(r.Context())
	if c == nil || c.Role != store.RoleAdmin {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}
	from, to := parseRange(r)
	if to <= from {
		writeError(w, http.StatusBadRequest, "invalid time range")
		return
	}
	segs, err := s.st.SegmentsInRange(id, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var freed int64
	delSeg, skipped := 0, 0
	for _, seg := range segs {
		if seg.Locked {
			skipped++
			continue
		}
		s.removeSegmentFile(seg)
		if s.st.DeleteSegment(seg.ID) == nil {
			freed += seg.SizeBytes
			delSeg++
		}
	}
	evs, _ := s.st.EventsInRange(id, from, to)
	delEv := 0
	for _, e := range evs {
		if e.ThumbPath != "" {
			_ = os.Remove(e.ThumbPath)
		}
		if e.SnapshotPath != "" {
			_ = os.Remove(e.SnapshotPath)
		}
		if s.st.DeleteEvent(e.ID) == nil {
			delEv++
		}
	}
	name := id
	if cam, err := s.st.GetCamera(id); err == nil {
		name = cam.Name
	}
	s.log.Info("recordings removed", "camera", id, "segments", delSeg,
		"events", delEv, "skippedLocked", skipped, "freedBytes", freed)
	s.logAudit(r, "recordings.remove", name, fmt.Sprintf("%d clips over %s", delSeg, time.Duration((to-from)*int64(time.Millisecond)).Round(time.Minute)))
	writeJSON(w, http.StatusOK, map[string]any{
		"deletedSegments": delSeg,
		"deletedEvents":   delEv,
		"skippedLocked":   skipped,
		"freedBytes":      freed,
	})
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
	// Local segments store an absolute path → serve straight from disk. Network
	// segments store a backend-relative path → stream through the backend with full
	// HTTP range support (seeking, partial content) via ServeContent.
	if filepath.IsAbs(seg.Path) {
		http.ServeFile(w, r, seg.Path)
		return
	}
	if s.registry == nil {
		writeError(w, http.StatusServiceUnavailable, "storage backend unavailable")
		return
	}
	backend, err := s.registry.For(seg.LocationID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "storage backend unavailable")
		return
	}
	rc, _, err := backend.Open(seg.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	defer rc.Close()
	// rc is a ReadSeeker; ServeContent handles range requests over it.
	http.ServeContent(w, r, segID+".ts", time.UnixMilli(seg.StartTS), rc)
}

// recordingsDir is the directory recordings currently live in: the operator
// override if set, otherwise the deploy default.
func (s *Server) recordingsDir() string {
	if o := s.st.RecordingsPathOverride(); o != "" {
		return o
	}
	return s.cfg.RecordingsDir
}

// dirBytes sums the sizes of all regular files under root (best-effort).
func dirBytes(root string) int64 {
	var total int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// appFilesBytes is everything Aperture stores that isn't a recorded .ts segment:
// the SQLite DB, exports, frame-cache, secrets, HomeKit state (all under DataDir)
// plus per-camera event thumbnails/snapshots under the recordings dir.
func (s *Server) appFilesBytes() int64 {
	total := dirBytes(s.cfg.DataDir)
	base := s.recordingsDir()
	if cams, err := s.st.ListCameras(); err == nil {
		for _, c := range cams {
			total += dirBytes(filepath.Join(base, c.ID, "events"))
		}
	}
	return total
}

// handleStorage reports usage scoped to the operator's allocation (the storage
// cap): per-camera recordings + Aperture's own files + free-within-allocation.
// Disk totals are still returned but secondary.
// handleBitrate returns the recorded-throughput series over a recent window (one
// point per complete minute), for the overview "Recording Bitrate" chart.
func (s *Server) handleBitrate(w http.ResponseWriter, r *http.Request) {
	mins := int64(30)
	if v, err := strconv.ParseInt(r.URL.Query().Get("minutes"), 10, 64); err == nil && v > 0 && v <= 240 {
		mins = v
	}
	const bucketMs int64 = 60_000
	now := time.Now().UnixMilli()
	end := (now / bucketMs) * bucketMs // exclude the in-progress minute
	start := end - mins*bucketMs
	buckets, err := s.st.BitrateBuckets(start, end, bucketMs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	series := make([]map[string]any, 0, mins)
	var curBps int64
	for t := start; t < end; t += bucketMs {
		bps := buckets[t] * 8 / (bucketMs / 1000) // bits/sec averaged over the minute
		series = append(series, map[string]any{"ts": t, "bps": bps})
		curBps = bps // last complete minute = "current"
	}
	writeJSON(w, http.StatusOK, map[string]any{"buckets": series, "currentBps": curBps})
}

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
	allocated := s.st.GetSettingInt64("storage_cap_bytes")
	appBytes := s.appFilesBytes()
	used := recorded + appBytes
	freeInAllocation := int64(0)
	if allocated > 0 && allocated > used {
		freeInAllocation = allocated - used
	}
	// Capacity estimate: at the actual encode bitrate of every recording camera
	// (continuous), how long the whole allocation holds.
	rateBps, recCams, _ := s.st.RecordingRateBps()
	var estFullSeconds int64
	if rateBps > 0 && allocated > 0 {
		estFullSeconds = allocated / rateBps
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recordedBytes":         recorded,
		"appBytes":              appBytes,
		"usedBytes":             used,
		"allocatedBytes":        allocated, // 0 = no allocation set (unlimited)
		"freeInAllocationBytes": freeInAllocation,
		"diskTotalBytes":        total,
		"diskFreeBytes":         free,
		"recordingRateBps":      rateBps,
		"recordingCameras":      recCams,
		"estimatedFullSeconds":  estFullSeconds,
		"cameras":               perCam,
		"path":                  s.recordingsDir(),
		"defaultPath":           s.cfg.RecordingsDir,
		"capBytes":              allocated,
		"dataDir":               s.cfg.DataDir, // fixed install location (read-only in UI)
		"locations":             s.allLocationViews(),
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

// diskBreakdown splits the whole recordings filesystem into Aperture's footprint
// (footage + app data), the reservation balloon, everything else ("other"), and
// what's free — plus the ceiling the operator may reserve (leaving the safety
// margin). This whole-disk view is surfaced ONLY to the allocation wizard.
func (s *Server) diskBreakdown() map[string]any {
	dir := s.recordingsDir()
	total, free := diskUsage(dir)
	recorded, _ := s.st.TotalBytes()
	appBytes := s.appFilesBytes()
	balloon := reserve.Size(dir)
	apertureUsed := recorded + appBytes
	other := total - free - apertureUsed - balloon
	if other < 0 {
		other = 0
	}
	maxAlloc := total - other - reserve.SafetyMargin
	if maxAlloc < 0 {
		maxAlloc = 0
	}
	return map[string]any{
		"totalBytes":          total,
		"freeBytes":           free,
		"otherBytes":          other,
		"apertureUsedBytes":   apertureUsed,
		"balloonBytes":        balloon,
		"allocatedBytes":      s.st.GetSettingInt64("storage_cap_bytes"),
		"maxAllocatableBytes": maxAlloc,
		"marginBytes":         reserve.SafetyMargin,
	}
}

// handleDiskStats reports the whole physical disk for the allocation wizard.
func (s *Server) handleDiskStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.diskBreakdown())
}

// handleReserveStorage saves the operator's hard allocation (clamped to what the
// disk can hold) and restarts the service so the reservation is applied on boot;
// the retention janitor then maintains the balloon file.
func (s *Server) handleReserveStorage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AllocatedBytes int64 `json:"allocatedBytes"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	bd := s.diskBreakdown()
	maxAlloc, _ := bd["maxAllocatableBytes"].(int64)
	if req.AllocatedBytes < 0 {
		req.AllocatedBytes = 0
	}
	if req.AllocatedBytes > maxAlloc {
		req.AllocatedBytes = maxAlloc
	}
	_ = s.st.SetSetting("storage_cap_bytes", strconv.FormatInt(req.AllocatedBytes, 10))
	s.logAudit(r, "storage.reserve", "", "allocated="+strconv.FormatInt(req.AllocatedBytes, 10))
	writeJSON(w, http.StatusOK, map[string]any{"allocatedBytes": req.AllocatedBytes, "restarting": true})
	s.scheduleRestart("storage allocation applied")
}

// handleRestart lets an admin bounce the service (the container supervisor
// restarts it — see restart: unless-stopped in compose).
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	s.logAudit(r, "system.restart", "", "operator restart")
	writeJSON(w, http.StatusOK, map[string]any{"restarting": true})
	s.scheduleRestart("operator requested")
}

// scheduleRestart exits the process shortly after the response flushes so the
// container supervisor brings it back on a fresh boot.
func (s *Server) scheduleRestart(reason string) {
	go func() {
		time.Sleep(900 * time.Millisecond)
		s.log.Warn("restarting service", "reason", reason)
		os.Exit(0)
	}()
}
