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
	minFreeBytesFloor = 5 << 30 // always try to keep at least 5 GiB free (see freeFloor)
	minFreeFraction   = 0.07    // ...and at least 7% of the disk
	deleteBatch       = 50
	motionGraceMs     = 120_000 // wait 2 min before pruning non-event footage

	// deleteFailureBudget is how long one pass may lose to failing deletes on a
	// location before leaving it alone until the next pass. A share that is down can
	// take a full dial timeout per delete, with every other camera waiting behind it.
	deleteFailureBudget = 30 * time.Second
	// maxStuck is how many undeletable items an oldest-first loop reads past in a
	// pass before giving up on the rest of it.
	maxStuck = 500
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
	// out from under itself. Retained as a fast in-memory belt alongside the
	// durable segment-lease table (T2.8), which is the general mechanism.
	activeExports []*store.ExportJob

	// leasesActive is set once per pass: true when any unexpired segment lease
	// exists, so delete() consults the lease table only when it could matter.
	leasesActive bool

	// du reports a filesystem's total and free bytes (diskUsage); tests replace it
	// so a pass does not depend on the machine's own disks.
	du func(path string) (total, free int64, ok bool)
	// shareDu is du for a network location (shareUsage); tests replace it too.
	shareDu func(locationID string) (total, free int64, ok bool)

	// deleteTrouble tracks failed deletes per location for one pass (see removeFile).
	deleteTrouble map[string]*deleteTrouble
}

// deleteTrouble is what went wrong deleting from one location during a pass.
type deleteTrouble struct {
	failed  int
	skipped int           // deletes not attempted once the budget ran out
	spent   time.Duration // time lost to failed deletes
	first   error
}

// New constructs a Janitor.
func New(cfg *config.Config, st *store.Store, reg *storage.Registry, log *slog.Logger) *Janitor {
	j := &Janitor{cfg: cfg, st: st, reg: reg, log: log, du: diskUsage}
	j.shareDu = j.shareUsage
	return j
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
	j.deleteTrouble = map[string]*deleteTrouble{}
	defer j.reportDeleteTrouble()
	// Snapshot in-flight exports once per pass so no prune path deletes footage a
	// render is about to read. A job that starts mid-pass is caught next tick.
	j.activeExports = j.freshExports()
	now := time.Now().UnixMilli()
	// Sweep expired leases, then note whether any live one remains so delete()
	// can skip the per-segment lease lookup entirely when there are none.
	_ = j.st.PurgeExpiredLeases(now)
	j.leasesActive, _ = j.st.HasActiveLeases(now)
	cams, err := j.st.ListCameras()
	if err != nil {
		j.log.Error("retention: list cameras", "err", err)
		return
	}
	for _, cam := range cams {
		// Motion-mode: keep only footage that overlaps an event (with pre/post-roll).
		if cam.RecordMode == store.ModeMotion {
			j.pruneNonMotion(cam.ID, now-motionGraceMs)
		}
		// With mirrored destinations a segment's lifetime is per LOCATION: it dies
		// everywhere only once it is older than every destination's retention
		// (0 = inherit the camera's days; any unlimited destination = immortal
		// until caps bite). Inside that window, per-destination retention removes
		// the copy on each location — promoting the canonical copy away first when
		// a fresher home survives — so "local keeps 1 day, NAS keeps 7" works.
		dests, _ := j.st.DestinationsForCamera(cam.ID)
		maxDays, unlimited := 0, false
		for _, d := range dests {
			days := d.RetentionDays
			if days <= 0 {
				days = cam.RetentionDays
			}
			if days <= 0 {
				unlimited = true
			} else if days > maxDays {
				maxDays = days
			}
		}
		if !unlimited && maxDays > 0 {
			j.deleteExpired(cam.ID, now-int64(maxDays)*86_400_000)
		}
		for _, d := range dests {
			days := d.RetentionDays
			if days <= 0 {
				days = cam.RetentionDays
			}
			if days > 0 {
				j.enforceDestinationRetention(cam.ID, d.LocationID, now-int64(days)*86_400_000)
			}
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
	// locations get a soft delete-oldest cap when an allocation is set, plus a
	// free-space net either way, because the share's disk is not only ours to fill.
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
			j.enforceShareFreeSpace(loc)
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
	deleted, stuck := 0, 0
	for total > target && stuck < maxStuck {
		// Whatever couldn't be evicted is still the oldest, so read past it.
		oldest, err := j.st.OldestEvictablesInLocation(locationID, deleteBatch+stuck)
		if err != nil || len(oldest) <= stuck {
			break
		}
		for _, ev := range oldest[stuck:] {
			if total <= target {
				break
			}
			if ev.IsCopy {
				// A mirror copy: freeing it loses nothing (the canonical survives
				// elsewhere) — always the cheapest eviction.
				if j.evictCopy(ev.Copy) {
					total -= ev.Copy.SizeBytes
					deleted++
				} else {
					stuck++
				}
				continue
			}
			// A canonical copy: if a mirror survives elsewhere, promote it so the
			// footage lives on and only THIS location's bytes are freed; otherwise
			// it is a full delete, exactly as before mirrors existed.
			if j.evictCanonical(ev.Seg) {
				total -= ev.Seg.SizeBytes
				deleted++
			} else {
				stuck++
			}
		}
	}
	if deleted > 0 {
		j.log.Warn("retention: over storage cap, removed oldest", "location", locationID, "count", deleted)
	}
}

// enforceDestinationRetention removes footage older than beforeMs from ONE
// location for ONE camera — the per-destination retention override. Mirror
// copies are simply deleted; a canonical copy is promoted to a surviving mirror
// first, and if no mirror exists yet it is left alone (the mirror pass will copy
// it within a tick and the next sweep promotes it) — footage another destination
// still wants is never destroyed by this pass.
func (j *Janitor) enforceDestinationRetention(cameraID, locationID string, beforeMs int64) {
	removed := 0
	if copies, err := j.st.ExpiredCopiesInLocation(cameraID, locationID, beforeMs, deleteBatch); err == nil {
		for _, c := range copies {
			if j.evictCopy(c) {
				removed++
			}
		}
	}
	if segs, err := j.st.ExpiredSegmentsInLocation(cameraID, locationID, beforeMs, deleteBatch); err == nil {
		for _, s := range segs {
			if j.exportLocked(s) {
				continue
			}
			copies, err := j.st.CopiesForSegment(s.ID)
			if err != nil || len(copies) == 0 {
				continue // no surviving home yet — the mirror pass fills one in
			}
			if j.promoteAndRemove(s, copies) {
				removed++
			}
		}
	}
	if removed > 0 {
		j.log.Info("retention: destination retention removed footage",
			"camera", cameraID, "location", locationID, "count", removed)
	}
}

// evictCanonical frees a canonical segment from its location: promote a mirror
// when one survives, else delete the segment entirely.
func (j *Janitor) evictCanonical(s *store.Segment) bool {
	copies, err := j.st.CopiesForSegment(s.ID)
	if err == nil && len(copies) > 0 {
		if j.exportLocked(s) {
			return false
		}
		return j.promoteAndRemove(s, copies)
	}
	return j.delete(s)
}

// promoteAndRemove promotes the first usable mirror copy into the segment row
// and removes the OLD canonical file. Local copies are preferred (cheaper to
// serve) over network ones.
func (j *Janitor) promoteAndRemove(s *store.Segment, copies []*store.SegmentCopy) bool {
	pick := copies[0]
	for _, c := range copies {
		if filepath.IsAbs(c.Path) {
			pick = c
			break
		}
	}
	// The old file goes first: once promoted, nothing records it, so a failed delete
	// would orphan it. A promote that fails after this is retried next pass, when
	// the file already being gone counts as removed.
	if !j.removeFile(s.Path, s.LocationID) {
		return false
	}
	if err := j.st.PromoteCopy(s.ID, pick.LocationID); err != nil {
		j.log.Error("retention: promote copy", "segment", s.ID, "err", err)
		return false
	}
	return true
}

// evictCopy removes a mirror copy, file first so a failed delete keeps its row.
func (j *Janitor) evictCopy(c *store.SegmentCopy) bool {
	if !j.removeFile(c.Path, c.LocationID) {
		return false
	}
	return j.st.DeleteSegmentCopy(c.SegmentID, c.LocationID) == nil
}

// removeFile deletes a footage file. A file that is already gone counts as removed.
//
// False means the file may still be there, so the caller must keep its row: the row
// is the only record of the file, and retention can never reclaim what it can't see.
// Dropping rows after failed deletes once left hundreds of GB of untracked footage on a
// share until it filled. The next pass retries.
func (j *Janitor) removeFile(path, locationID string) bool {
	if path == "" {
		return true
	}
	t := j.deleteTrouble[locationID]
	if t != nil && t.spent >= deleteFailureBudget {
		t.skipped++
		return false
	}
	start := time.Now()
	err := j.removeNow(path, locationID)
	if err == nil {
		return true
	}
	if t == nil {
		if j.deleteTrouble == nil {
			j.deleteTrouble = map[string]*deleteTrouble{}
		}
		t = &deleteTrouble{first: err}
		j.deleteTrouble[locationID] = t
	}
	t.failed++
	t.spent += time.Since(start)
	return false
}

// removeNow deletes one file. Local footage holds an absolute path; network footage
// holds a backend-relative path routed through the storage registry.
func (j *Janitor) removeNow(path, locationID string) error {
	if filepath.IsAbs(path) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if j.reg == nil {
		return nil
	}
	b, err := j.reg.For(locationID)
	if err != nil {
		return err
	}
	return b.Remove(path)
}

// reportDeleteTrouble logs a pass's failed deletes once per location, not per file:
// a share that is down would otherwise fail thousands of deletes every pass.
func (j *Janitor) reportDeleteTrouble() {
	for locationID, t := range j.deleteTrouble {
		j.log.Warn("retention: could not delete footage, keeping it to retry next pass",
			"location", locationID, "failed", t.failed, "notTried", t.skipped, "err", t.first)
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
	removed := 0
	for _, s := range segs {
		if j.delete(s) {
			removed++
		}
	}
	if removed > 0 {
		j.log.Info("retention: pruned non-motion footage", "camera", cameraID, "count", removed)
	}
}

func (j *Janitor) deleteExpired(cameraID string, beforeMs int64) {
	segs, err := j.st.ExpiredSegments(cameraID, beforeMs)
	if err != nil {
		return
	}
	removed := 0
	for _, s := range segs {
		if j.delete(s) {
			removed++
		}
	}
	if removed > 0 {
		j.log.Info("retention: aged out segments", "camera", cameraID, "count", removed)
	}
}

func (j *Janitor) enforceQuota(cameraID string, quota int64) {
	stat, err := j.st.CameraStat(cameraID)
	if err != nil {
		return
	}
	bytes := stat.Bytes
	deleted, stuck := 0, 0
	for bytes > quota && stuck < maxStuck {
		// Whatever couldn't be deleted is still the oldest, so read past it.
		oldest, err := j.st.OldestSegments(cameraID, deleteBatch+stuck)
		if err != nil || len(oldest) <= stuck {
			break
		}
		for _, s := range oldest[stuck:] {
			if bytes <= quota {
				break
			}
			if j.delete(s) {
				bytes -= s.SizeBytes
				deleted++
			} else {
				stuck++
			}
		}
	}
	if deleted > 0 {
		j.log.Info("retention: over quota, removed oldest", "camera", cameraID, "count", deleted)
	}
}

func (j *Janitor) enforceFreeSpace() {
	// Only footage on the default location frees space on this disk; footage on
	// other locations lives on their own filesystems.
	if n := j.freeSpace(store.DefaultLocationID, freeFloor, j.diskAt(j.activeDir())); n > 0 {
		j.log.Warn("retention: low disk space, removed oldest on default location", "count", n)
	}
}

// enforceLocationFreeSpace is the free-space safety net for an unallocated local
// location: it frees that location's oldest footage to keep its filesystem above
// the same floor the default location uses.
func (j *Janitor) enforceLocationFreeSpace(loc *store.StorageLocation) {
	path, _ := loc.Config["path"].(string)
	if path == "" {
		return
	}
	if n := j.freeSpace(loc.ID, freeFloor, j.diskAt(path)); n > 0 {
		j.log.Warn("retention: low disk space on location, removed oldest", "location", loc.ID, "count", n)
	}
}

// enforceShareFreeSpace is the free-space safety net for a network share. An
// allocation only limits what Aperture has indexed, and the disk behind a share has
// other things on it, so a full share would otherwise surface only as failed uploads.
func (j *Janitor) enforceShareFreeSpace(loc *store.StorageLocation) {
	usage := func() (int64, int64, bool) { return j.shareDu(loc.ID) }
	total, free, ok := usage()
	if !ok || total == 0 || free >= shareFreeFloor(total) {
		return
	}
	removed := j.freeSpace(loc.ID, shareFreeFloor, usage)
	if _, now, ok := usage(); ok {
		free = now
	}
	j.log.Warn("retention: network share is nearly full", "location", loc.ID, "name", loc.Name,
		"freeBytes", free, "floorBytes", shareFreeFloor(total), "removed", removed)
}

// shareUsage reports a network location's capacity. A share that can't be asked is
// skipped quietly: failed uploads already say so, every few seconds.
func (j *Janitor) shareUsage(locationID string) (total, free int64, ok bool) {
	if j.reg == nil {
		return 0, 0, false
	}
	b, err := j.reg.For(locationID)
	if err != nil {
		return 0, 0, false
	}
	st, err := b.Stat()
	if err != nil || !st.Supported {
		return 0, 0, false
	}
	return st.Total, st.Free, true
}

// diskAt adapts du to one path, for freeSpace.
func (j *Janitor) diskAt(path string) func() (int64, int64, bool) {
	return func() (int64, int64, bool) { return j.du(path) }
}

// freeSpace evicts from one location until usage reports it back above floor, and
// returns how many items it removed. Mirror copies parked on the location go first:
// each is redundant, so freeing it loses nothing. Only then is canonical footage
// evicted, oldest first, through evictCanonical, which promotes a surviving mirror
// instead of destroying it; footage with no mirror is deleted, still subject to
// delete's export and lease protection.
func (j *Janitor) freeSpace(locationID string, floor func(total int64) int64, usage func() (total, free int64, ok bool)) int {
	total, free, ok := usage()
	if !ok || total == 0 {
		return 0
	}
	minFree := floor(total)
	removed, stuckCopies, stuckSegs := 0, 0, 0
	for free < minFree && stuckCopies+stuckSegs < maxStuck {
		// Whatever couldn't be evicted is still the oldest, so read past it.
		copies, err := j.st.OldestCopiesInLocation(locationID, deleteBatch+stuckCopies)
		if err != nil {
			break
		}
		if len(copies) > stuckCopies {
			for _, c := range copies[stuckCopies:] {
				if j.evictCopy(c) {
					removed++
				} else {
					stuckCopies++
				}
			}
		} else {
			oldest, err := j.st.OldestSegmentsInLocation(locationID, deleteBatch+stuckSegs)
			if err != nil || len(oldest) <= stuckSegs {
				break
			}
			for _, s := range oldest[stuckSegs:] {
				if j.evictCanonical(s) {
					removed++
				} else {
					stuckSegs++
				}
			}
		}
		if _, free, ok = usage(); !ok {
			break
		}
	}
	return removed
}

// delete removes a segment's bytes and index row. It returns false without deleting
// the row when the segment is protected by an in-flight export (see exportLocked) or
// its file couldn't be removed; callers that free space in a loop must treat a false
// result as "no progress" so they don't spin re-selecting the same batch.
func (j *Janitor) delete(s *store.Segment) bool {
	if j.exportLocked(s) {
		return false
	}
	// Full death: a segment deleted here dies EVERYWHERE — remove every mirror
	// copy (file + row) before the canonical file and row.
	if copies, err := j.st.CopiesForSegment(s.ID); err == nil {
		for _, c := range copies {
			if !j.evictCopy(c) {
				return false // the segment's row is what brings the next pass back here
			}
		}
	}
	// A held, unexpired lease overlapping this segment protects it (T2.8). Only
	// consulted when a lease exists at all (leasesActive), so the common no-lease
	// path adds no query.
	if j.leasesActive {
		if held, err := j.st.LeaseOverlaps(s.CameraID, s.StartTS, s.EndTS, time.Now().UnixMilli()); err == nil && held {
			return false
		}
	}
	if !j.removeFile(s.Path, s.LocationID) {
		return false
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

// freeFloor is how much the free-space safety nets keep free on a filesystem of
// the given size: 7% of it and at least 5 GiB, but never more than a quarter of
// it. Without that cap a filesystem smaller than the 5 GiB floor (a USB stick, a
// small partition) could never satisfy it, and the net would delete every
// segment on it on every pass.
func freeFloor(total int64) int64 {
	floor := int64(minFreeBytesFloor)
	if q := total / 4; floor > q {
		floor = q
	}
	if f := int64(float64(total) * minFreeFraction); f > floor {
		floor = f
	}
	return floor
}

// shareFreeFloor is how much the net keeps free on a network share: the 5 GiB floor
// with freeFloor's quarter cap, but no percentage. A NAS pool holds everything else
// on that NAS, and 7% of 20 TB would have cameras' footage deleted to defend 1.4 TB
// nobody reserved for them.
func shareFreeFloor(total int64) int64 {
	if q := total / 4; q < minFreeBytesFloor {
		return q
	}
	return minFreeBytesFloor
}

// diskUsage returns total and available bytes for the filesystem at path.
func diskUsage(path string) (total, free int64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize), true
}
