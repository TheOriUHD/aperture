package store

// TrackPoint is where one recognised object was at one moment.
type TrackPoint struct {
	EventID    string  `json:"eventId"`
	Obj        int64   `json:"obj"`
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	W          float64 `json:"w"`
	H          float64 `json:"h"`
	TS         int64   `json:"ts"`
}

// AddTrackPoints stores a batch of an event's object positions.
func (s *Store) AddTrackPoints(cameraID string, pts []TrackPoint) error {
	if len(pts) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(`INSERT INTO object_tracks (event_id, camera_id, obj, label, confidence, x, y, w, h, ts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range pts {
		if _, err := stmt.Exec(p.EventID, cameraID, p.Obj, p.Label, p.Confidence, p.X, p.Y, p.W, p.H, p.TS); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TrackPoints returns a camera's object positions between from and to (unix
// ms), oldest first, at most limit rows.
func (s *Store) TrackPoints(cameraID string, from, to int64, limit int) ([]TrackPoint, error) {
	rows, err := s.db.Query(
		`SELECT event_id, obj, label, confidence, x, y, w, h, ts FROM object_tracks
		 WHERE camera_id = ? AND ts >= ? AND ts <= ? ORDER BY ts LIMIT ?`, cameraID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrackPoint{}
	for rows.Next() {
		var p TrackPoint
		if err := rows.Scan(&p.EventID, &p.Obj, &p.Label, &p.Confidence, &p.X, &p.Y, &p.W, &p.H, &p.TS); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
