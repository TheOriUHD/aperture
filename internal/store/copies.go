package store

import (
	"database/sql"
	"time"
)

// SegmentCopy is one extra physical copy of a segment on another storage
// location (a mirror). The segment row itself is the canonical copy; when the
// canonical copy is evicted while a mirror survives, PromoteCopy folds the
// mirror into the segment row so playback keeps working transparently.
type SegmentCopy struct {
	SegmentID  string
	LocationID string
	Path       string // absolute (local location) or backend-relative (network)
	SizeBytes  int64
	CreatedAt  int64 // unix ms
}

// InsertSegmentCopy records a completed mirror copy.
func (s *Store) InsertSegmentCopy(c *SegmentCopy) error {
	if c.CreatedAt == 0 {
		c.CreatedAt = time.Now().UnixMilli()
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO segment_copies (segment_id, location_id, path, size_bytes, created_at)
         VALUES (?, ?, ?, ?, ?)`,
		c.SegmentID, c.LocationID, c.Path, c.SizeBytes, c.CreatedAt)
	return err
}

// CopiesForSegment lists a segment's mirror copies (serving fallback + cascade
// delete).
func (s *Store) CopiesForSegment(segmentID string) ([]*SegmentCopy, error) {
	rows, err := s.db.Query(
		`SELECT segment_id, location_id, path, size_bytes, created_at FROM segment_copies WHERE segment_id = ?`,
		segmentID)
	if err != nil {
		return nil, err
	}
	return collectCopies(rows)
}

// DeleteSegmentCopy removes one copy row (the file is the caller's job).
func (s *Store) DeleteSegmentCopy(segmentID, locationID string) error {
	_, err := s.db.Exec(`DELETE FROM segment_copies WHERE segment_id = ? AND location_id = ?`, segmentID, locationID)
	return err
}

// SegmentsNeedingCopy returns segments of a camera that should exist on a mirror
// location but don't yet: canonical copy elsewhere, no copy row for the target,
// newer than sinceMs (the mirror's retention horizon — copying footage the
// destination would immediately age out is wasted transfer), not locked. Newest
// first, so a live dashboard mirror stays current and history backfills behind it.
func (s *Store) SegmentsNeedingCopy(cameraID, locationID string, sinceMs int64, limit int) ([]*Segment, error) {
	rows, err := s.db.Query(segmentSelect+
		` WHERE camera_id = ? AND location_id != ? AND start_ts >= ? AND locked = 0
          AND NOT EXISTS (SELECT 1 FROM segment_copies c WHERE c.segment_id = segments.id AND c.location_id = ?)
          ORDER BY start_ts DESC LIMIT ?`,
		cameraID, locationID, sinceMs, locationID, limit)
	if err != nil {
		return nil, err
	}
	return collectSegments(rows)
}

// PromoteCopy makes a mirror copy the canonical one: the segment row takes the
// copy's location and path, and the copy row disappears. Called when retention
// evicts the canonical copy while this mirror must survive — footage a
// destination still wants is never lost just because another destination let go.
func (s *Store) PromoteCopy(segmentID, locationID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var path string
	if err := tx.QueryRow(
		`SELECT path FROM segment_copies WHERE segment_id = ? AND location_id = ?`,
		segmentID, locationID).Scan(&path); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE segments SET location_id = ?, path = ? WHERE id = ?`,
		locationID, path, segmentID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM segment_copies WHERE segment_id = ? AND location_id = ?`,
		segmentID, locationID); err != nil {
		return err
	}
	return tx.Commit()
}

// TotalCopyBytesInLocation sums the mirror bytes physically on a location.
// Location capacity accounting = canonical segments there + copies there.
func (s *Store) TotalCopyBytesInLocation(locationID string) (int64, error) {
	var v sql.NullInt64
	err := s.db.QueryRow(
		`SELECT COALESCE(SUM(size_bytes),0) FROM segment_copies WHERE location_id = ?`, locationID).Scan(&v)
	return v.Int64, err
}

// TotalCopyBytes sums all mirror bytes everywhere (global physical usage).
func (s *Store) TotalCopyBytes() (int64, error) {
	var v sql.NullInt64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(size_bytes),0) FROM segment_copies`).Scan(&v)
	return v.Int64, err
}

// ExpiredCopiesInLocation returns copies on a location whose segment started
// before beforeMs — the per-destination retention pass.
func (s *Store) ExpiredCopiesInLocation(cameraID, locationID string, beforeMs int64, limit int) ([]*SegmentCopy, error) {
	rows, err := s.db.Query(
		`SELECT c.segment_id, c.location_id, c.path, c.size_bytes, c.created_at
         FROM segment_copies c JOIN segments s ON s.id = c.segment_id
         WHERE s.camera_id = ? AND c.location_id = ? AND s.start_ts < ? AND s.locked = 0
         ORDER BY s.start_ts ASC LIMIT ?`,
		cameraID, locationID, beforeMs, limit)
	if err != nil {
		return nil, err
	}
	return collectCopies(rows)
}

// Evictable is one physically-evictable unit on a location: either a canonical
// segment (IsCopy=false) or a mirror copy (IsCopy=true). The janitor's per-
// location cap walks these oldest-first regardless of kind.
type Evictable struct {
	Seg    *Segment
	Copy   *SegmentCopy // set when IsCopy
	IsCopy bool
}

// OldestEvictablesInLocation merges the oldest canonical segments and the oldest
// mirror copies on a location into one oldest-first list of at most limit items.
func (s *Store) OldestEvictablesInLocation(locationID string, limit int) ([]Evictable, error) {
	prim, err := s.OldestSegmentsInLocation(locationID, limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT c.segment_id, c.location_id, c.path, c.size_bytes, c.created_at
         FROM segment_copies c JOIN segments s ON s.id = c.segment_id
         WHERE c.location_id = ? AND s.locked = 0
         ORDER BY s.start_ts ASC LIMIT ?`, locationID, limit)
	if err != nil {
		return nil, err
	}
	copies, err := collectCopies(rows)
	if err != nil {
		return nil, err
	}
	// Resolve each copy's segment for start-ts ordering + camera attribution.
	out := make([]Evictable, 0, len(prim)+len(copies))
	for _, p := range prim {
		out = append(out, Evictable{Seg: p})
	}
	for _, c := range copies {
		seg, err := s.SegmentByID(c.SegmentID)
		if err != nil {
			continue
		}
		out = append(out, Evictable{Seg: seg, Copy: c, IsCopy: true})
	}
	// Merge-sort by segment start (stable enough: ties keep insertion order).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Seg.StartTS < out[j-1].Seg.StartTS; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func collectCopies(rows *sql.Rows) ([]*SegmentCopy, error) {
	defer rows.Close()
	var out []*SegmentCopy
	for rows.Next() {
		var c SegmentCopy
		if err := rows.Scan(&c.SegmentID, &c.LocationID, &c.Path, &c.SizeBytes, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// ExpiredSegmentsInLocation returns canonical segments of a camera physically on
// a location and older than beforeMs (per-destination retention: these should
// leave THIS location, which for a canonical copy means promotion elsewhere).
func (s *Store) ExpiredSegmentsInLocation(cameraID, locationID string, beforeMs int64, limit int) ([]*Segment, error) {
	rows, err := s.db.Query(segmentSelect+
		` WHERE camera_id = ? AND location_id = ? AND start_ts < ? AND locked = 0
          ORDER BY start_ts ASC LIMIT ?`,
		cameraID, locationID, beforeMs, limit)
	if err != nil {
		return nil, err
	}
	return collectSegments(rows)
}

// DeleteCopiesInLocation drops every copy row on a location (files are gone with
// the location itself — used by location force-delete).
func (s *Store) DeleteCopiesInLocation(locationID string) error {
	_, err := s.db.Exec(`DELETE FROM segment_copies WHERE location_id = ?`, locationID)
	return err
}
