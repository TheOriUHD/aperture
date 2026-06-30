// Package retention enforces recording retention: per-camera age limits and
// storage quotas (overwrite-oldest), plus a global free-space safety net.
package retention

import (
	"context"
	"log/slog"
	"os"
	"syscall"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
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
	log *slog.Logger
}

// New constructs a Janitor.
func New(cfg *config.Config, st *store.Store, log *slog.Logger) *Janitor {
	return &Janitor{cfg: cfg, st: st, log: log}
}

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
	j.enforceGlobalCap()
	j.enforceFreeSpace()
}

// activeDir is the directory recordings live in (operator override or default).
func (j *Janitor) activeDir() string {
	if o := j.st.RecordingsPathOverride(); o != "" {
		return o
	}
	return j.cfg.RecordingsDir
}

// enforceGlobalCap deletes the oldest footage across all cameras to keep total
// recordings under the operator's global storage cap (0 = unlimited).
func (j *Janitor) enforceGlobalCap() {
	capBytes := j.st.GetSettingInt64("storage_cap_bytes")
	if capBytes <= 0 {
		return
	}
	total, err := j.st.TotalBytes()
	if err != nil {
		return
	}
	deleted := 0
	for total > capBytes {
		oldest, err := j.st.OldestSegmentsGlobal(deleteBatch)
		if err != nil || len(oldest) == 0 {
			break
		}
		for _, s := range oldest {
			if total <= capBytes {
				break
			}
			j.delete(s)
			total -= s.SizeBytes
			deleted++
		}
	}
	if deleted > 0 {
		j.log.Warn("retention: over global storage cap, removed oldest", "count", deleted)
	}
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
		for _, s := range oldest {
			if bytes <= quota {
				break
			}
			j.delete(s)
			bytes -= s.SizeBytes
			deleted++
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
		oldest, err := j.st.OldestSegmentsGlobal(deleteBatch)
		if err != nil || len(oldest) == 0 {
			break
		}
		for _, s := range oldest {
			j.delete(s)
			deleted++
		}
		if _, free, ok = diskUsage(j.cfg.RecordingsDir); !ok {
			break
		}
	}
	if deleted > 0 {
		j.log.Warn("retention: low disk space, removed oldest globally", "count", deleted)
	}
}

func (j *Janitor) delete(s *store.Segment) {
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		j.log.Error("retention: remove file", "path", s.Path, "err", err)
	}
	if err := j.st.DeleteSegment(s.ID); err != nil {
		j.log.Error("retention: delete row", "id", s.ID, "err", err)
	}
}

// diskUsage returns total and available bytes for the filesystem at path.
func diskUsage(path string) (total, free int64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize), true
}
