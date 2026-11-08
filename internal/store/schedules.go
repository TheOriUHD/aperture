package store

// Schedule is one weekly recording window for a camera (scheduled mode).
type Schedule struct {
	ID       string `json:"id"`
	CameraID string `json:"-"`
	Weekday  int    `json:"weekday"`  // 0=Sunday .. 6=Saturday
	StartMin int    `json:"startMin"` // minutes from midnight
	EndMin   int    `json:"endMin"`
	Mode     string `json:"mode"`
}

// ListSchedules returns a camera's schedule windows.
func (s *Store) ListSchedules(cameraID string) ([]*Schedule, error) {
	rows, err := s.db.Query(
		`SELECT id, camera_id, weekday, start_min, end_min, mode FROM schedules
         WHERE camera_id = ? ORDER BY weekday, start_min`, cameraID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Schedule
	for rows.Next() {
		var sc Schedule
		if err := rows.Scan(&sc.ID, &sc.CameraID, &sc.Weekday, &sc.StartMin, &sc.EndMin, &sc.Mode); err != nil {
			return nil, err
		}
		out = append(out, &sc)
	}
	return out, rows.Err()
}

// SetSchedules replaces a camera's schedule windows.
func (s *Store) SetSchedules(cameraID string, scheds []*Schedule) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM schedules WHERE camera_id = ?`, cameraID); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, sc := range scheds {
		mode := sc.Mode
		if mode == "" {
			mode = ModeContinuous
		}
		if _, err := tx.Exec(
			`INSERT INTO schedules (id, camera_id, weekday, start_min, end_min, mode)
             VALUES (?, ?, ?, ?, ?, ?)`,
			NewID(), cameraID, sc.Weekday, sc.StartMin, sc.EndMin, mode); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// RecordModeSettable reports whether a remote control (Home Assistant, an
// automation) may switch a camera to mode. "scheduled" needs at least one recording
// window: the recorder runs only inside a window, so with none the switch would
// silently stop the camera recording, with nobody at a screen to notice.
func (s *Store) RecordModeSettable(cameraID, mode string) bool {
	if !ValidRecordMode(mode) {
		return false
	}
	if mode != ModeScheduled {
		return true
	}
	scheds, err := s.ListSchedules(cameraID)
	return err == nil && len(scheds) > 0
}
