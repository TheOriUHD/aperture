package store

import (
	"database/sql"
	"errors"
)

// Segment is one recorded media file on disk.
type Segment struct {
	ID         string `json:"id"`
	CameraID   string `json:"cameraId"`
	Path       string `json:"-"`
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
		`INSERT INTO segments (id, camera_id, path, start_ts, end_ts, duration_ms, size_bytes, has_motion, locked)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seg.ID, seg.CameraID, seg.Path, seg.StartTS, seg.EndTS, seg.DurationMS, seg.SizeBytes,
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

// OldestSegmentsGlobal returns up to limit unlocked segments across all cameras,
// oldest first. Used by the global free-space safety net.
func (s *Store) OldestSegmentsGlobal(limit int) ([]*Segment, error) {
	rows, err := s.db.Query(segmentSelect+
		` WHERE locked = 0 ORDER BY start_ts ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	return collectSegments(rows)
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

const segmentSelect = `SELECT id, camera_id, path, start_ts, end_ts, duration_ms, size_bytes, has_motion, locked FROM segments`

func scanSegment(row rowScanner) (*Segment, error) {
	var seg Segment
	var motion, locked int
	err := row.Scan(&seg.ID, &seg.CameraID, &seg.Path, &seg.StartTS, &seg.EndTS,
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
