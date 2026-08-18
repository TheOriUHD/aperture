package record

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// Mirroring: a camera may list extra storage destinations beyond its primary
// (camera_destinations). ffmpeg still writes exactly ONCE — to the primary — and
// this pass copies completed, indexed segments to every mirror destination,
// recording a segment_copies row per copy. Because it works from the indexed
// canonical file, the sacred record path is untouched: a slow or dead mirror can
// never stall or break recording, it just falls behind and backfills when the
// destination returns (the copy row is only written after a successful copy, so
// missing copies are retried on later ticks automatically).

// mirrorBatchPerDest bounds copies per destination per tick so one tick's work
// stays small; the loop drains the backlog across ticks.
const mirrorBatchPerDest = 12

// mirrorOnce copies missing segments to every camera's mirror destinations.
func (r *Recorder) mirrorOnce() {
	cams, err := r.st.ListCameras()
	if err != nil {
		return
	}
	for _, cam := range cams {
		mirrors, err := r.st.MirrorDestinationsForCamera(cam.ID)
		if err != nil || len(mirrors) == 0 {
			continue
		}
		for _, dest := range mirrors {
			loc, err := r.st.GetStorageLocation(dest.LocationID)
			if err != nil || !loc.Enabled {
				continue
			}
			// Horizon: only mirror what this destination would actually keep.
			days := dest.RetentionDays
			if days <= 0 {
				days = cam.RetentionDays
			}
			var since int64
			if days > 0 {
				since = time.Now().UnixMilli() - int64(days)*86_400_000
			}
			segs, err := r.st.SegmentsNeedingCopy(cam.ID, dest.LocationID, since, mirrorBatchPerDest)
			if err != nil {
				continue
			}
			copied := 0
			for _, seg := range segs {
				if err := r.copySegment(seg, loc); err != nil {
					// First failure aborts this destination for the tick (it is
					// probably down); everything missing is retried next tick.
					r.log.Warn("mirror: copy failed", "camera", cam.ID, "location", loc.Name, "err", err)
					break
				}
				copied++
			}
			if copied > 0 {
				r.log.Info("mirror: copied segments", "camera", cam.Name, "location", loc.Name, "count", copied)
			}
		}
	}
}

// copySegment materialises one segment on a mirror location and records the copy
// row. Local mirror → plain file copy; network mirror → backend.Put.
func (r *Recorder) copySegment(seg *store.Segment, loc *store.StorageLocation) error {
	src, err := r.openSegment(seg)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer src.Close()

	name := strconv.FormatInt(seg.StartTS/1000, 10) + ".ts"
	switch loc.Type {
	case store.LocationLocal:
		root, _ := loc.Config["path"].(string)
		if root == "" {
			return fmt.Errorf("local location %s has no path", loc.ID)
		}
		dir := filepath.Join(root, seg.CameraID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		dst := filepath.Join(dir, name)
		// Copy to a temp name then rename, so a half-written file is never indexed.
		tmp := dst + ".part"
		f, err := os.Create(tmp)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, src); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, dst); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		return r.st.InsertSegmentCopy(&store.SegmentCopy{
			SegmentID: seg.ID, LocationID: loc.ID, Path: dst, SizeBytes: seg.SizeBytes,
		})
	case store.LocationSMB, store.LocationSFTP:
		if r.reg == nil {
			return fmt.Errorf("no storage registry")
		}
		b, err := r.reg.For(loc.ID)
		if err != nil {
			return err
		}
		// Backend.Put wants a local file path. The canonical copy usually IS a
		// local file; a network-primary canonical is staged to a temp file first.
		localPath := seg.Path
		if !filepath.IsAbs(localPath) {
			tmp, err := os.CreateTemp("", "aperture-mirror-*.ts")
			if err != nil {
				return err
			}
			defer func() { _ = os.Remove(tmp.Name()) }()
			if _, err := io.Copy(tmp, src); err != nil {
				_ = tmp.Close()
				return err
			}
			if err := tmp.Close(); err != nil {
				return err
			}
			localPath = tmp.Name()
		}
		rel := filepath.ToSlash(filepath.Join(seg.CameraID, name))
		if err := b.EnsureDir(rel); err != nil {
			return err
		}
		if _, err := b.Put(localPath, rel); err != nil {
			return err
		}
		return r.st.InsertSegmentCopy(&store.SegmentCopy{
			SegmentID: seg.ID, LocationID: loc.ID, Path: rel, SizeBytes: seg.SizeBytes,
		})
	default:
		return fmt.Errorf("unknown location type %q", loc.Type)
	}
}

// openSegment opens the canonical copy of a segment for reading, wherever it
// lives (absolute local path, or via the storage registry for network primaries).
func (r *Recorder) openSegment(seg *store.Segment) (io.ReadCloser, error) {
	if filepath.IsAbs(seg.Path) {
		return os.Open(seg.Path)
	}
	if r.reg == nil {
		return nil, fmt.Errorf("no storage registry for remote segment")
	}
	b, err := r.reg.For(seg.LocationID)
	if err != nil {
		return nil, err
	}
	rc, _, err := b.Open(seg.Path)
	return rc, err
}
