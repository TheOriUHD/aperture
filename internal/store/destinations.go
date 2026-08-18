package store

import "time"

// CameraDestination is one (camera, storage location) membership. A camera has
// one PRIMARY destination — cameras.location_id, where ffmpeg writes — and any
// number of mirror memberships that receive copies of completed segments.
// RetentionDays 0 means "inherit the camera's retentionDays"; a non-zero value
// overrides it FOR THAT LOCATION ONLY (e.g. local keeps 1 day, NAS keeps 7).
type CameraDestination struct {
	CameraID      string `json:"cameraId"`
	LocationID    string `json:"locationId"`
	RetentionDays int    `json:"retentionDays"`
}

// DestinationsForCamera lists every membership of a camera, primary included.
// The primary is guaranteed present even if its row is missing (self-heals a
// pre-migration camera), so callers can rely on len >= 1.
func (s *Store) DestinationsForCamera(cameraID string) ([]CameraDestination, error) {
	cam, err := s.GetCamera(cameraID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT camera_id, location_id, retention_days FROM camera_destinations WHERE camera_id = ?`, cameraID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CameraDestination
	havePrimary := false
	for rows.Next() {
		var d CameraDestination
		if err := rows.Scan(&d.CameraID, &d.LocationID, &d.RetentionDays); err != nil {
			return nil, err
		}
		if d.LocationID == cam.LocationID {
			havePrimary = true
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !havePrimary {
		out = append(out, CameraDestination{CameraID: cameraID, LocationID: cam.LocationID})
	}
	return out, nil
}

// MirrorDestinationsForCamera is DestinationsForCamera minus the primary — the
// set the recorder's mirror pass must fill with copies.
func (s *Store) MirrorDestinationsForCamera(cameraID string) ([]CameraDestination, error) {
	cam, err := s.GetCamera(cameraID)
	if err != nil {
		return nil, err
	}
	all, err := s.DestinationsForCamera(cameraID)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, d := range all {
		if d.LocationID != cam.LocationID {
			out = append(out, d)
		}
	}
	return out, nil
}

// SetCameraDestinations replaces a camera's full membership set atomically and
// points the primary at primaryLocationID. The primary must be one of the
// memberships (enforced here, not trusted from the caller).
func (s *Store) SetCameraDestinations(cameraID, primaryLocationID string, dests []CameraDestination) error {
	found := false
	for _, d := range dests {
		if d.LocationID == primaryLocationID {
			found = true
			break
		}
	}
	if !found {
		dests = append(dests, CameraDestination{CameraID: cameraID, LocationID: primaryLocationID})
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM camera_destinations WHERE camera_id = ?`, cameraID); err != nil {
		return err
	}
	for _, d := range dests {
		rd := d.RetentionDays
		if rd < 0 {
			rd = 0
		}
		if _, err := tx.Exec(
			`INSERT INTO camera_destinations (camera_id, location_id, retention_days) VALUES (?, ?, ?)`,
			cameraID, d.LocationID, rd); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE cameras SET location_id = ?, updated_at = ? WHERE id = ?`,
		primaryLocationID, time.Now().Unix(), cameraID); err != nil {
		return err
	}
	return tx.Commit()
}

// DestinationsForLocation lists the memberships pointing at one location — the
// cameras recording (primary) or mirroring there.
func (s *Store) DestinationsForLocation(locationID string) ([]CameraDestination, error) {
	rows, err := s.db.Query(
		`SELECT camera_id, location_id, retention_days FROM camera_destinations WHERE location_id = ?`, locationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CameraDestination
	for rows.Next() {
		var d CameraDestination
		if err := rows.Scan(&d.CameraID, &d.LocationID, &d.RetentionDays); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RemoveDestinationsForLocation drops every membership of a location (used when
// the location itself is deleted). Cameras whose PRIMARY pointed there are the
// caller's problem (ReassignCameras already handles that path).
func (s *Store) RemoveDestinationsForLocation(locationID string) error {
	_, err := s.db.Exec(`DELETE FROM camera_destinations WHERE location_id = ?`, locationID)
	return err
}
