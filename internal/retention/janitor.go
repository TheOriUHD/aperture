// Package retention enforces recording retention: per-camera age limits and
// storage quotas (overwrite-oldest), plus a global free-space safety net.
package retention

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/reserve"
	"github.com/TheOriUHD/aperture/internal/storage"
	"github.com/TheOriUHD/aperture/internal/store"
)

const (
	interval          = 5 * time.Minute
	minFreeBytesFloor = 5 << 30 // always try to keep at least 5 GiB free
	minFreeFraction   = 0.07    // ...and at least 7% of the disk
	deleteBatch       = 50
	motionGraceMs     = 120_000 // wait 2 min before pruning non-event footage
)

// Janitor periodically prunes recorded segments.
type Janitor struct {
	cfg *config.Config
	st  *store.Store
	reg *storage.Registry
	log *slog.Logger
	mu  sync.Mutex // serializes enforce() so a manual trigger can't overlap the loop

	// activeExports is snapshotted at the start of each enforce() pass; segments
	// overlapping a queued/running export are skipped so a render is never pruned
	// out from under itself. Interim guard until the segment-lease table (T2.8)
	// replaces the inert `locked` flag with a real, expiring lease.
	activeExports []*store.ExportJob
}

// New constructs a Janitor.
func New(cfg *config.Config, st *store.Store, reg *storage.Registry, log *slog.Logger) *Janitor {
	return &Janitor{cfg: cfg, st: st, reg: reg, log: log}
}

// EnforceNow runs a retention + reservation pass immediately (e.g. right after an
// allocation change) so balloons resize and over-cap footage is trimmed without
// waiting for the next tick. Safe to call concurrently with the loop.
func (j *Janitor) EnforceNow() { j.enforce() }

// Run enforces retention immediately and then on a fixed interval until ctx is
// cancelled.
func (j *Janitor) Run(ctx context.Context) {
	j.enforce()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			j.enforce()
		}
	}
}

func (j *Janitor) enforce() {
	j.mu.Lock()
	defer j.mu.Unlock()
	// Snapshot in-flight exports once per pass so no prune path deletes footage a
	// render is about to read. A job that starts mid-pass is caught next tick.
	j.activeExports = j.freshExports()
	cams, err := j.st.ListCameras()
	if err != nil {
		j.log.Error("retention: list cameras", "err", err)
		return
	}
	now := time.Now().UnixMilli()
	for _, cam := range cams {
		// Motion-mode: keep only footage that overlaps an event (with pre/post-roll).
		if cam.RecordMode == store.ModeMotion {
			j.pruneNonMotion(cam.ID, now-motionGraceMs)
		}
		if cam.RetentionDays > 0 {
			before := now - int64(cam.RetentionDays)*86_400_000
			j.deleteExpired(cam.ID, before)
		}
		if cam.StorageQuotaBytes > 0 {
			j.enforceQuota(cam.ID, cam.StorageQuotaBytes)
		}
	}
	// Default location: cap = storage_cap_bytes and its footprint includes app
	// data (DB, thumbnails). The free-space net only applies in "unlimited" mode —
	// with a hard allocation the cap + balloon already bound the footprint down to
	// reserve.SafetyMargin.
	defaultCap := j.st.GetSettingInt64("storage_cap_bytes")
	j.enforceLocationCap(store.DefaultLocationID, defaultCap, true)
	if defaultCap <= 0 {
		j.enforceFreeSpace()
	}
	// Each configured location enforces its own allocation independently. Local
	// locations get a hard cap (or a free-space net when unallocated); network
	// locations get a soft delete-oldest cap when an allocation is set.
	locs, _ := j.st.ListStorageLocations()
	for _, loc := range locs {
		if !loc.Enabled {
			continue
		}
		switch loc.Type {
		case store.LocationLocal:
			if p, _ := loc.Config["path"].(string); p == "" {
				continue
			}
			if loc.AllocationBytes > 0 {
				j.enforceLocationCap(loc.ID, loc.AllocationBytes, false)
			} else {
				j.enforceLocationFreeSpace(loc)
			}
		case store.LocationSMB, store.LocationSFTP:
			if loc.AllocationBytes > 0 {
				j.enforceLocationCap(loc.ID, loc.AllocationBytes, false)
			}
		}
	}
	j.reconcileReservation()
}

// reconcileReservation resizes each local location's hard-reservation balloon so
// its footprint (footage [+ app data, for the default] + balloon) equals that
// location's allocation. With no allocation set the balloon is removed.
func (j *Janitor) reconcileReservation() {
	// Default location: allocation = storage_cap_bytes; footprint = its footage + app data.
	allocation := j.st.GetSettingInt64("storage_cap_bytes")
	if recorded, err := j.st.TotalBytesInLocation(store.DefaultLocationID); err == nil {
		used := recorded + j.apertureAppBytes()
		if _, err := reserve.Reconcile(j.activeDir(), allocation, used); err != nil {
			j.log.Warn("retention: reservation reconcile failed", "err", err)
		}
	}
	// Each configured local location: allocation = AllocationBytes; footprint = its footage.
	for _, loc := range j.localLocations() {
		path, _ := loc.Config["path"].(string)
		used, err := j.st.TotalBytesInLocation(loc.ID)
		if err != nil {
			continue
		}
		if _, err := reserve.Reconcile(path, loc.AllocationBytes, used); err != nil {
			j.log.Warn("retention: location reservation reconcile failed", "location", loc.ID, "err", err)
		}
	}
}

// localLocations returns the configured, enabled local storage locations (each
// with a usable path). The built-in default is handled separately — it carries
// app data and its cap comes from storage_cap_bytes, not the locations table.
func (j *Janitor) localLocations() []*store.StorageLocation {
	locs, err := j.st.ListStorageLocations()
	if err != nil {
		return nil
	}
	var out []*store.StorageLocation
	for _, l := range locs {
		if l.Type != store.LocationLocal || !l.Enabled {
			continue
		}
		if p, _ := l.Config["path"].(string); p != "" {
			out = append(out, l)
		}
	}
	return out
}

// dirBytes sums the sizes of all regular files under root.
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

// activeDir is the directory recordings live in (operator override or default).
func (j *Janitor) activeDir() string {
	if o := j.st.RecordingsPathOverride(); o != "" {
		return o
	}
	return j.cfg.RecordingsDir
}

// enforceLocationCap deletes the oldest footage on a single location to keep its
// footage under that location's allocation (0 = unlimited). For the default
// location includeApp is true: the allocation is Aperture's TOTAL footprint
// budget, so footage is capped at allocation - app data (DB, thumbnails), keeping
// reported "used" (footage + app) at or under the allocation instead of a hair over.
func (j *Janitor) enforceLocationCap(locationID string, capBytes int64, includeApp bool) {
	if capBytes <= 0 {
		return
	}
	target := capBytes
	if includeApp {
		target -= j.apertureAppBytes()
	}
	if target < 0 {
		target = 0
	}
	total, err := j.st.TotalBytesInLocation(locationID)
	if err != nil {
		return
	}
	deleted := 0
	for total > target {
		oldest, err := j.st.OldestSegmentsInLocation(locationID, deleteBatch)
		if err != nil || len(oldest) == 0 {
			break
		}
		progressed := false
		for _, s := range oldest {
			if total <= target {
				break
			}
			if j.delete(s) {
				total -= s.SizeBytes
				deleted++
				progressed = true
			}
		}
		if !progressed {
			break // whole batch is export-protected; retry next pass
		}
	}
	if deleted > 0 {
		j.log.Warn("retention: over storage cap, removed oldest", "location", locationID, "count", deleted)
	}
}

// apertureAppBytes mirrors the API's appFilesBytes: Aperture's non-footage
// footprint (DB, exports, caches under DataDir + per-camera event thumbnails).
func (j *Janitor) apertureAppBytes() int64 {
	total := dirBytes(j.cfg.DataDir)
	base := j.activeDir()
	if cams, err := j.st.ListCameras(); err == nil {
		for _, c := range cams {
			total += dirBytes(filepath.Join(base, c.ID, "events"))
		}
	}
	return total
}

// pruneNonMotion deletes a motion-mode camera's segments that don't overlap any
// event. The grace period lets in-progress events finalize first.
func (j *Janitor) pruneNonMotion(cameraID string, beforeMs int64) {
	segs, err := j.st.NonEventSegments(cameraID, beforeMs)
	if err != nil {
		return
	}
	for _, s := range segs {
		j.delete(s)
	}
	if len(segs) > 0 {
		j.log.Info("retention: pruned non-motion footage", "camera", cameraID, "count", len(segs))
	}
}

func (j *Janitor) deleteExpired(cameraID string, beforeMs int64) {
	segs, err := j.st.ExpiredSegments(cameraID, beforeMs)
	if err != nil {
		return
	}
	for _, s := range segs {
		j.delete(s)
	}
	if len(segs) > 0 {
		j.log.Info("retention: aged out segments", "camera", cameraID, "count", len(segs))
	}
}

func (j *Janitor) enforceQuota(cameraID string, quota int64) {
	stat, err := j.st.CameraStat(cameraID)
	if err != nil {
		return
	}
	bytes := stat.Bytes
	deleted := 0
	for bytes > quota {
		oldest, err := j.st.OldestSegments(cameraID, deleteBatch)
		if err != nil || len(oldest) == 0 {
			break
		}
		progressed := false
		for _, s := range oldest {
			if bytes <= quota {
				break
			}
			if j.delete(s) {
				bytes -= s.SizeBytes
				deleted++
				progressed = true
			}
		}
		if !progressed {
			break // whole batch is export-protected; retry next pass
		}
	}
	if deleted > 0 {
		j.log.Info("retention: over quota, removed oldest", "camera", cameraID, "count", deleted)
	}
}

func (j *Janitor) enforceFreeSpace() {
	total, free, ok := diskUsage(j.activeDir())
	if !ok || total == 0 {
		return
	}
	minFree := int64(float64(total) * minFreeFraction)
	if minFree < minFreeBytesFloor {
		minFree = minFreeBytesFloor
	}
	if free >= minFree {
		return
	}
	deleted := 0
	for free < minFree {
		// Only footage on the default location frees space on this disk; footage
		// on other locations lives on their own filesystems.
		oldest, err := j.st.OldestSegmentsInLocation(store.DefaultLocationID, deleteBatch)
		if err != nil || len(oldest) == 0 {
			break
		}
		progressed := false
		for _, s := range oldest {
			if j.delete(s) {
				deleted++
				progressed = true
			}
		}
		if !progressed {
			break // whole batch is export-protected; retry next pass
		}
		if _, free, ok = diskUsage(j.activeDir()); !ok {
			break
		}
	}
	if deleted > 0 {
		j.log.Warn("retention: low disk space, removed oldest on default location", "count", deleted)
	}
}

// enforceLocationFreeSpace is the free-space safety net for an unallocated local
// location: it deletes that location's oldest footage to keep its filesystem
// above the same floor the default location uses.
func (j *Janitor) enforceLocationFreeSpace(loc *store.StorageLocation) {
	path, _ := loc.Config["path"].(string)
	if path == "" {
		return
	}
	total, free, ok := diskUsage(path)
	if !ok || total == 0 {
		return
	}
	minFree := int64(float64(total) * minFreeFraction)
	if minFree < minFreeBytesFloor {
		minFree = minFreeBytesFloor
	}
	if free >= minFree {
		return
	}
	deleted := 0
	for free < minFree {
		oldest, err := j.st.OldestSegmentsInLocation(loc.ID, deleteBatch)
		if err != nil || len(oldest) == 0 {
			break
		}
		progressed := false
		for _, s := range oldest {
			if j.delete(s) {
				deleted++
				progressed = true
			}
		}
		if !progressed {
			break // whole batch is export-protected; retry next pass
		}
		if _, free, ok = diskUsage(path); !ok {
			break
		}
	}
	if deleted > 0 {
		j.log.Warn("retention: low disk space on location, removed oldest", "location", loc.ID, "count", deleted)
	}
}

// delete removes a segment's bytes and index row. It returns false without
// deleting when the segment is protected by an in-flight export (see
// exportLocked); callers that free space in a loop must treat a false result as
// "no progress" so they don't spin re-selecting the same protected batch.
func (j *Janitor) delete(s *store.Segment) bool {
	if j.exportLocked(s) {
		return false
	}
	// Local segments hold an absolute path; network segments hold a backend-
	// relative path routed through the storage registry.
	if s.Path != "" && filepath.IsAbs(s.Path) {
		if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
			j.log.Error("retention: remove file", "path", s.Path, "err", err)
		}
	} else if s.Path != "" && j.reg != nil {
		if b, err := j.reg.For(s.LocationID); err == nil {
			_ = b.Remove(s.Path)
		}
	}
	if err := j.st.DeleteSegment(s.ID); err != nil {
		j.log.Error("retention: delete row", "id", s.ID, "err", err)
	}
	return true
}

// exportMaxProtectAge bounds how long an export may shield its segment range from
// retention. Renders run under the app context with no per-job timeout, and a job
// stuck in `running` is only cleared by the 24h export TTL sweep — so without this
// bound a hung render could hold footage for ~a day, a disk-full risk on a
// near-full volume. 2h sits well above any realistic single render (even a
// full-window burn-in/timelapse transcode) yet is a small fraction of the 24h TTL.
const exportMaxProtectAge = 2 * time.Hour

// freshExports returns the queued/running exports still young enough to protect
// footage. Any job older than exportMaxProtectAge is dropped (and logged once per
// pass) so a stuck render can't hold footage on a near-full disk; its segments
// become prunable regardless of the job's state.
func (j *Janitor) freshExports() []*store.ExportJob {
	all, err := j.st.PendingExports()
	if err != nil || len(all) == 0 {
		return nil
	}
	nowSec := time.Now().Unix()
	maxAgeSec := int64(exportMaxProtectAge / time.Second)
	out := make([]*store.ExportJob, 0, len(all))
	for _, job := range all {
		if job.CreatedAt > 0 && nowSec-job.CreatedAt > maxAgeSec {
			j.log.Warn("retention: export exceeded max protection age; its footage is now prunable (render likely stuck)",
				"export", job.ID, "status", job.Status, "ageSec", nowSec-job.CreatedAt)
			continue
		}
		out = append(out, job)
	}
	return out
}

// exportLocked reports whether a segment overlaps the time window of any queued
// or running export for the same camera. Interim protection against pruning
// footage a render is about to read; superseded by the segment-lease table.
func (j *Janitor) exportLocked(s *store.Segment) bool {
	for _, job := range j.activeExports {
		if s.StartTS >= job.ToTS || s.EndTS <= job.FromTS {
			continue // no time overlap
		}
		for _, cid := range job.Cameras() {
			if cid == s.CameraID {
				return true
			}
		}
	}
	return false
}

// diskUsage returns total and available bytes for the filesystem at path.
func diskUsage(path string) (total, free int64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize), true
}
