package store

// Segment leases (T2.8). A lease shields a camera's footage in [FromTS,ToTS)
// from retention until ExpiresAt. The janitor consults LeaseOverlaps before
// deleting any segment; a long consumer (export, playback, extension) takes a
// lease for the window it needs and releases it when done. Leases are a range
// table — never a per-segment flag — so protecting footage writes O(1) rows, not
// O(segments) (plan C4).

// Lease is one held protection window.
type Lease struct {
	ID        string `json:"id"`
	CameraID  string `json:"cameraId"`
	FromTS    int64  `json:"fromTs"`
	ToTS      int64  `json:"toTs"`
	Holder    string `json:"holder"`
	ExpiresAt int64  `json:"expiresAt"`
}

// CreateLease inserts a lease and returns it (ID assigned). All times are unix ms.
func (s *Store) CreateLease(cameraID string, fromTS, toTS, expiresAt int64, holder string) (*Lease, error) {
	l := &Lease{ID: NewID(), CameraID: cameraID, FromTS: fromTS, ToTS: toTS, Holder: holder, ExpiresAt: expiresAt}
	_, err := s.db.Exec(
		`INSERT INTO segment_leases (id, camera_id, from_ts, to_ts, holder, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		l.ID, l.CameraID, l.FromTS, l.ToTS, l.Holder, l.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// GetLease returns a lease by id.
func (s *Store) GetLease(id string) (*Lease, error) {
	var l Lease
	err := s.db.QueryRow(
		`SELECT id, camera_id, from_ts, to_ts, holder, expires_at FROM segment_leases WHERE id = ?`, id).
		Scan(&l.ID, &l.CameraID, &l.FromTS, &l.ToTS, &l.Holder, &l.ExpiresAt)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// ReleaseLease drops a lease by id (idempotent — a missing id is not an error).
func (s *Store) ReleaseLease(id string) error {
	_, err := s.db.Exec(`DELETE FROM segment_leases WHERE id = ?`, id)
	return err
}

// LeaseOverlaps reports whether an unexpired lease for cameraID overlaps the
// half-open segment window [segStart,segEnd) as of now (all unix ms). The
// overlap test is `from_ts < segEnd AND to_ts > segStart`.
func (s *Store) LeaseOverlaps(cameraID string, segStart, segEnd, now int64) (bool, error) {
	var exists int
	err := s.db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM segment_leases
		 WHERE camera_id = ? AND expires_at > ? AND from_ts < ? AND to_ts > ?)`,
		cameraID, now, segEnd, segStart).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists == 1, nil
}

// HasActiveLeases reports whether any unexpired lease exists, letting the janitor
// skip per-segment lease checks entirely in the common (no-lease) case.
func (s *Store) HasActiveLeases(now int64) (bool, error) {
	var exists int
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM segment_leases WHERE expires_at > ?)`, now).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists == 1, nil
}

// PurgeExpiredLeases deletes leases whose deadline has passed.
func (s *Store) PurgeExpiredLeases(now int64) error {
	_, err := s.db.Exec(`DELETE FROM segment_leases WHERE expires_at <= ?`, now)
	return err
}
