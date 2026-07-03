package store

import "strings"

// EventObject is one detected object within an event (bbox normalized 0..1).
type EventObject struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	W          float64 `json:"w"`
	H          float64 `json:"h"`
	TS         int64   `json:"ts,omitempty"`
}

// SaveEventDetections replaces an event's objects and updates its summary
// columns in one transaction. topLabel is the headline label; summary is a short
// human list like "person, car".
func (s *Store) SaveEventDetections(eventID, topLabel, summary string, objs []EventObject) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM event_objects WHERE event_id = ?`, eventID); err != nil {
		return err
	}
	for _, o := range objs {
		if _, err := tx.Exec(
			`INSERT INTO event_objects (event_id, label, confidence, bbox_x, bbox_y, bbox_w, bbox_h, ts)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			eventID, o.Label, o.Confidence, o.X, o.Y, o.W, o.H, o.TS); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE events SET top_label = ?, object_summary = ? WHERE id = ?`,
		topLabel, summary, eventID); err != nil {
		return err
	}
	return tx.Commit()
}

// EventObjects returns the detected objects for a single event.
func (s *Store) EventObjects(eventID string) ([]EventObject, error) {
	rows, err := s.db.Query(
		`SELECT label, confidence, bbox_x, bbox_y, bbox_w, bbox_h, ts
		 FROM event_objects WHERE event_id = ? ORDER BY confidence DESC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventObject
	for rows.Next() {
		var o EventObject
		if err := rows.Scan(&o.Label, &o.Confidence, &o.X, &o.Y, &o.W, &o.H, &o.TS); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// EventObjectsFor batch-loads objects for many events (one query, grouped by
// event id) so list views can render boxes without an N+1 fan-out.
func (s *Store) EventObjectsFor(ids []string) (map[string][]EventObject, error) {
	out := map[string][]EventObject{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.Query(
		`SELECT event_id, label, confidence, bbox_x, bbox_y, bbox_w, bbox_h, ts
		 FROM event_objects WHERE event_id IN (`+ph+`) ORDER BY confidence DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var o EventObject
		if err := rows.Scan(&id, &o.Label, &o.Confidence, &o.X, &o.Y, &o.W, &o.H, &o.TS); err != nil {
			return nil, err
		}
		out[id] = append(out[id], o)
	}
	return out, rows.Err()
}
