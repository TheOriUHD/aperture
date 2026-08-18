package store

import (
	"database/sql"
	"errors"
)

// Segment is one recorded media file on disk.
type Segment struct {
	ID         string `json:"id"`
	CameraID   string `json:"cameraId"`
	LocationID string `json:"-"` // storage location this file lives on ("" = default root)
	Path       string `json:"-"` // backend-relative for added locations; absolute for the default
	StartTS    int64  `json:"startTs"` // unix ms
	EndTS      int64  `json:"endTs"`   // unix ms
	DurationMS int64  `json:"durationMs"`
	SizeBytes  int64  `json:"sizeBytes"`
	HasMotion  bool   `json:"hasMotion"`
	Locked     bool   `json:"locked"`
}

// InsertSegment indexes a newly-recorded segment.
func (s *Store) InsertSegment(seg *Segment) error {
	if seg.ID == "" {
		seg.ID = NewID()
	}
	_, err := s.db.Exec(
		`INSERT INTO segments (id, camera_id, location_id, path, start_ts, end_ts, duration_ms, size_bytes, has_motion, locked)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seg.ID, seg.CameraID, seg.LocationID, seg.Path, seg.StartTS, seg.EndTS, seg.DurationMS, seg.SizeBytes,
		boolInt(seg.HasMotion), boolInt(seg.Locked))
	return err
}

// SegmentByID fetches a segment (used to serve its file).
func (s *Store) SegmentByID(id string) (*Segment, error) {
	return scanSegment(s.db.QueryRow(segmentSelect+` WHERE id = ?`, id))
}

// ListSegments returns segments overlapping [fromMs, toMs] for a camera, ordered
// by start. Used by the timeline and HLS playlist generation.
func (s *Store) ListSegments(cameraID string, fromMs, toMs int64) ([]*Segment, error) {
	rows, err := s.db.Query(segmentSelect+
		` WHERE camera_id = ? AND start_ts < ? AND end_ts > ? ORDER BY start_ts ASC`,
		cameraID, toMs, fromMs)
	if err != nil {
		return nil, err
	}
	return collectSegments(rows)
}

// ListSegmentsPage returns up to limit segments for a camera overlapping
// [fromMs,toMs], ordered (start_ts,id) ascending and starting strictly after the
// (cursorStart,cursorID) keyset position — cursorStart<=0 && cursorID=="" starts
// from the beginning. Segment ids are random, so (start_ts,id) is the stable
// keyset: paging never dups or skips even as segments are recorded or pruned
// between fetches. Backs the /segments inspection endpoint (T2.3); the render
// path uses coverage (T2.1) instead.
func (s *Store) ListSegmentsPage(cameraID string, fromMs, toMs, cursorStart int64, cursorID string, limit int) ([]*Segment, error) {
	q := segmentSelect + ` WHERE camera_id = ? AND start_ts < ? AND end_ts > ?`
	args := []any{cameraID, toMs, fromMs}
	if cursorStart > 0 || cursorID != "" {
		q += ` AND (start_ts > ? OR (start_ts = ? AND id > ?))`
		args = append(args, cursorStart, cursorStart, cursorID)
	}
	q += ` ORDER BY start_ts ASC, id ASC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	return collectSegments(rows)
}

// RecordingGraceMs is how stale the newest segment may be before a camera in a
// recording mode is treated as having stopped delivering. Shared by the health
// handler and the watchdog (kept here in store to avoid an import cycle).
const RecordingGraceMs = 90_000

// Gap is an uncovered interval (unix ms) in a camera's recorded coverage.
type Gap struct {
	StartTS int64 `json:"startTs"`
	EndTS   int64 `json:"endTs"`
}

// DayCoverage returns the recorded coverage in milliseconds within [fromMs,toMs]
// for a camera, merging segments separated by <=1s (matching the HLS gap
// threshold). Used to compute the health "coverage %" for continuous cameras.
func (s *Store) DayCoverage(cameraID string, fromMs, toMs int64) (coveredMs int64, err error) {
	segs, err := s.ListSegments(cameraID, fromMs, toMs)
	if err != nil {
		return 0, err
	}
	var curStart, curEnd int64 = -1, -1
	flush := func() {
		if curStart >= 0 {
			coveredMs += curEnd - curStart
		}
	}
	for _, seg := range segs {
		st, en := seg.StartTS, seg.EndTS
		if st < fromMs {
			st = fromMs
		}
		if en > toMs {
			en = toMs
		}
		if en <= st {
			continue
		}
		if curStart < 0 {
			curStart, curEnd = st, en
			continue
		}
		if st <= curEnd+1000 {
			if en > curEnd {
				curEnd = en
			}
		} else {
			flush()
			curStart, curEnd = st, en
		}
	}
	flush()
	return coveredMs, nil
}

// SegmentStarts returns the set of start_ts (unix ms) already indexed for a
// camera, so a re-index can skip files that are already known.
func (s *Store) SegmentStarts(cameraID string) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT start_ts FROM segments WHERE camera_id = ?`, cameraID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var t int64
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out[t] = true
	}
	return out, rows.Err()
}

// InsertSegmentsBatch indexes many segments in one transaction — used by the
// re-index recovery, where inserting tens of thousands of rows one-by-one would
// be far too slow.
func (s *Store) InsertSegmentsBatch(segs []*Segment) error {
	if len(segs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	stmt, err := tx.Prepare(`INSERT INTO segments
		(id, camera_id, location_id, path, start_ts, end_ts, duration_ms, size_bytes, has_motion, locked)
		VALUES (?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, seg := range segs {
		if seg.ID == "" {
			seg.ID = NewID()
		}
		if _, err := stmt.Exec(seg.ID, seg.CameraID, seg.LocationID, seg.Path, seg.StartTS, seg.EndTS,
			seg.DurationMS, seg.SizeBytes, boolInt(seg.HasMotion), boolInt(seg.Locked)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MaxSegmentStart returns the latest indexed segment start (unix ms) for a
// camera, or 0. Used to seed the indexer so it skips already-known files.
func (s *Store) MaxSegmentStart(cameraID string) (int64, error) {
	var v sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(start_ts) FROM segments WHERE camera_id = ?`, cameraID).Scan(&v)
	if err != nil {
		return 0, err
	}
	return v.Int64, nil
}

// ExpiredSegments returns unlocked segments that ended before beforeMs.
func (s *Store) ExpiredSegments(cameraID string, beforeMs int64) ([]*Segment, error) {
	rows, err := s.db.Query(segmentSelect+
		` WHERE camera_id = ? AND end_ts < ? AND locked = 0 ORDER BY start_ts ASC`,
		cameraID, beforeMs)
	if err != nil {
		return nil, err
	}
	return collectSegments(rows)
}

// OldestSegments returns up to limit unlocked segments, oldest first.
func (s *Store) OldestSegments(cameraID string, limit int) ([]*Segment, error) {
	rows, err := s.db.Query(segmentSelect+
		` WHERE camera_id = ? AND locked = 0 ORDER BY start_ts ASC LIMIT ?`,
		cameraID, limit)
	if err != nil {
		return nil, err
	}
	return collectSegments(rows)
}

// SegmentsInRange returns segments overlapping [fromMs, toMs]. An empty
// cameraID matches all cameras. Used by admin storage purge.
func (s *Store) SegmentsInRange(cameraID string, fromMs, toMs int64) ([]*Segment, error) {
	q := segmentSelect + ` WHERE start_ts < ? AND end_ts > ?`
	args := []any{toMs, fromMs}
	if cameraID != "" {
		q = segmentSelect + ` WHERE camera_id = ? AND start_ts < ? AND end_ts > ?`
		args = []any{cameraID, toMs, fromMs}
	}
	rows, err := s.db.Query(q+` ORDER BY start_ts ASC`, args...)
	if err != nil {
		return nil, err
	}
	return collectSegments(rows)
}

// OldestSegmentsInLocation returns up to limit unlocked segments physically on a
// storage location, oldest first — used by per-location soft-cap trimming.
func (s *Store) OldestSegmentsInLocation(locationID string, limit int) ([]*Segment, error) {
	rows, err := s.db.Query(segmentSelect+
		` WHERE location_id = ? AND locked = 0 ORDER BY start_ts ASC LIMIT ?`, locationID, limit)
	if err != nil {
		return nil, err
	}
	return collectSegments(rows)
}

// TotalBytesInLocation sums footage bytes stored on a location.
func (s *Store) TotalBytesInLocation(locationID string) (int64, error) {
	var v sql.NullInt64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(size_bytes),0) FROM segments WHERE location_id = ?`, locationID).Scan(&v)
	return v.Int64, err
}

// CountSegmentsInLocation counts segments stored on a location (delete guard).
func (s *Store) CountSegmentsInLocation(locationID string) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM segments WHERE location_id = ?`, locationID).Scan(&n)
	return n, err
}

// NonEventSegments returns a camera's unlocked segments that ended before
// beforeMs and do not overlap any motion event. Used to prune motion-mode
// cameras down to just their event clips (with pre/post-roll).
func (s *Store) NonEventSegments(cameraID string, beforeMs int64) ([]*Segment, error) {
	rows, err := s.db.Query(segmentSelect+`
        WHERE camera_id = ? AND end_ts < ? AND locked = 0
          AND NOT EXISTS (
            SELECT 1 FROM events e
            WHERE e.camera_id = segments.camera_id
              AND e.start_ts < segments.end_ts
              AND (e.end_ts IS NULL OR e.end_ts > segments.start_ts)
          )
        ORDER BY start_ts ASC`, cameraID, beforeMs)
	if err != nil {
		return nil, err
	}
	return collectSegments(rows)
}

// BitrateBuckets sums recorded bytes into time buckets (keyed by bucket-start
// unix ms, by segment end time) across all cameras — the recording-throughput
// series for the overview chart.
func (s *Store) BitrateBuckets(fromMs, toMs, bucketMs int64) (map[int64]int64, error) {
	if bucketMs <= 0 {
		bucketMs = 60000
	}
	rows, err := s.db.Query(`
		SELECT (end_ts / ?) * ? AS bucket, COALESCE(SUM(size_bytes), 0)
		FROM segments
		WHERE end_ts >= ? AND end_ts < ?
		GROUP BY bucket`, bucketMs, bucketMs, fromMs, toMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var bucket, bytes int64
		if err := rows.Scan(&bucket, &bytes); err != nil {
			return nil, err
		}
		out[bucket] = bytes
	}
	return out, rows.Err()
}

// DeleteSegment removes a segment row by id.
func (s *Store) DeleteSegment(id string) error {
	_, err := s.db.Exec(`DELETE FROM segments WHERE id = ?`, id)
	return err
}

// CameraStat aggregates a camera's recorded footage.
type CameraStat struct {
	CameraID string `json:"cameraId"`
	Bytes    int64  `json:"bytes"`
	Count    int64  `json:"count"`
	OldestTS int64  `json:"oldestTs"`
	NewestTS int64  `json:"newestTs"`
}

// CameraStat returns storage aggregates for one camera.
func (s *Store) CameraStat(cameraID string) (*CameraStat, error) {
	st := &CameraStat{CameraID: cameraID}
	var b, c, oldest, newest sql.NullInt64
	err := s.db.QueryRow(
		`SELECT COALESCE(SUM(size_bytes),0), COUNT(*), MIN(start_ts), MAX(end_ts)
         FROM segments WHERE camera_id = ?`, cameraID).Scan(&b, &c, &oldest, &newest)
	if err != nil {
		return nil, err
	}
	st.Bytes, st.Count, st.OldestTS, st.NewestTS = b.Int64, c.Int64, oldest.Int64, newest.Int64
	return st, nil
}

// TotalBytes returns the total recorded bytes across all cameras.
func (s *Store) TotalBytes() (int64, error) {
	var b sql.NullInt64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(size_bytes),0) FROM segments`).Scan(&b)
	return b.Int64, err
}

// RecordingRateBps estimates the combined write rate in bytes/sec if every
// camera that has footage recorded continuously at its measured encode bitrate
// (SUM(size)/SUM(duration) per camera). That reflects the real resolution/fps/
// codec each stream actually delivers, ignoring any per-camera motion or
// retention limits — i.e. the worst-case fill rate. Also returns the number of
// cameras contributing.
func (s *Store) RecordingRateBps() (bps int64, cameras int, err error) {
	rows, err := s.db.Query(`
		SELECT COALESCE(SUM(size_bytes),0), COALESCE(SUM(duration_ms),0)
		FROM segments GROUP BY camera_id HAVING SUM(duration_ms) > 0`)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var total float64
	for rows.Next() {
		var bytes, durMs int64
		if err := rows.Scan(&bytes, &durMs); err != nil {
			return 0, 0, err
		}
		if durMs > 0 {
			total += float64(bytes) / float64(durMs) * 1000 // bytes per second of footage
			cameras++
		}
	}
	return int64(total), cameras, rows.Err()
}

const segmentSelect = `SELECT id, camera_id, location_id, path, start_ts, end_ts, duration_ms, size_bytes, has_motion, locked FROM segments`

func scanSegment(row rowScanner) (*Segment, error) {
	var seg Segment
	var motion, locked int
	err := row.Scan(&seg.ID, &seg.CameraID, &seg.LocationID, &seg.Path, &seg.StartTS, &seg.EndTS,
		&seg.DurationMS, &seg.SizeBytes, &motion, &locked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	seg.HasMotion = motion != 0
	seg.Locked = locked != 0
	return &seg, nil
}

func collectSegments(rows *sql.Rows) ([]*Segment, error) {
	defer rows.Close()
	var out []*Segment
	for rows.Next() {
		seg, err := scanSegment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, seg)
	}
	return out, rows.Err()
}

// EvictionPreview describes what lowering a location's allocation would destroy.
// It mirrors the janitor's oldest-first arithmetic (see Janitor.enforceLocationCap)
// WITHOUT deleting anything, so the UI can state real numbers before the user
// commits — lowering an allocation removes footage immediately and irreversibly.
type EvictionPreview struct {
	Bytes    int64
	Segments int64
	OldestTS int64
	NewestTS int64
	PerCamera map[string]*EvictionCamera
}

// EvictionCamera is the per-camera share of an eviction.
type EvictionCamera struct {
	Bytes    int64
	Segments int64
	OldestTS int64
	NewestTS int64
}

// PreviewEvictionInLocation walks a location's segments oldest-first and reports
// which would be removed to bring usage down to targetBytes. Locked segments (held
// by an active export) are skipped, exactly as the janitor skips them.
func (s *Store) PreviewEvictionInLocation(locationID string, targetBytes int64) (*EvictionPreview, error) {
	total, err := s.TotalBytesInLocation(locationID)
	if err != nil {
		return nil, err
	}
	p := &EvictionPreview{PerCamera: map[string]*EvictionCamera{}}
	if total <= targetBytes {
		return p, nil // nothing to do — this is a no-op or an increase
	}
	rows, err := s.db.Query(segmentSelect+
		` WHERE location_id = ? AND locked = 0 ORDER BY start_ts ASC`, locationID)
	if err != nil {
		return nil, err
	}
	segs, err := collectSegments(rows)
	if err != nil {
		return nil, err
	}
	for _, seg := range segs {
		if total <= targetBytes {
			break
		}
		total -= seg.SizeBytes
		p.Bytes += seg.SizeBytes
		p.Segments++
		if p.OldestTS == 0 || seg.StartTS < p.OldestTS {
			p.OldestTS = seg.StartTS
		}
		if seg.EndTS > p.NewestTS {
			p.NewestTS = seg.EndTS
		}
		c := p.PerCamera[seg.CameraID]
		if c == nil {
			c = &EvictionCamera{}
			p.PerCamera[seg.CameraID] = c
		}
		c.Bytes += seg.SizeBytes
		c.Segments++
		if c.OldestTS == 0 || seg.StartTS < c.OldestTS {
			c.OldestTS = seg.StartTS
		}
		if seg.EndTS > c.NewestTS {
			c.NewestTS = seg.EndTS
		}
	}
	return p, nil
}
