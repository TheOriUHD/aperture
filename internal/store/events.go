package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Event is a detected motion event (a clip with pre/post-roll).
type Event struct {
	ID           string  `json:"id"`
	Seq          int64   `json:"seq"` // monotonic cursor (strictly increasing, not gapless)
	CameraID     string  `json:"cameraId"`
	Type         string  `json:"type"`
	StartTS      int64   `json:"startTs"` // unix ms
	EndTS        int64   `json:"endTs"`   // unix ms, 0 while ongoing
	ThumbPath    string  `json:"-"`
	SnapshotPath string  `json:"-"`
	Score        float64 `json:"score"`
	Reviewed     bool    `json:"reviewed"`
	Saved        bool    `json:"saved"`

	// Object detection (opt-in). TopLabel is the highest-confidence label,
	// ObjectSummary a short "person, car" list for the row. Labels/Objects are
	// transient (populated by the detector or a batch load), not stored inline.
	TopLabel      string        `json:"topLabel,omitempty"`
	ObjectSummary string        `json:"objectSummary,omitempty"`
	Labels        []string      `json:"-"`
	Objects       []EventObject `json:"objects,omitempty"`

	// MotionCells is the union of coarse motion-map cells (indices into a
	// motion.SearchW×SearchH grid) active during the event — the basis for Grid
	// Search. Empty for events recorded before this existed, or audio events.
	MotionCells []int `json:"motionCells,omitempty"`
}

// InsertEvent creates a new event (end_ts NULL while ongoing).
func (s *Store) InsertEvent(e *Event) error {
	if e.ID == "" {
		e.ID = NewID()
	}
	if e.Type == "" {
		e.Type = "motion"
	}
	e.Seq = s.eventSeq.Add(1)
	_, err := s.db.Exec(
		`INSERT INTO events (id, camera_id, type, start_ts, end_ts, thumb_path, snapshot_path, score, reviewed, saved, seq)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.CameraID, e.Type, e.StartTS, nullInt(e.EndTS), e.ThumbPath, e.SnapshotPath,
		e.Score, boolInt(e.Reviewed), boolInt(e.Saved), e.Seq)
	return err
}

// FinalizeEvent sets an event's end time, final score, and motion-cell map (the
// active cell indices, JSON-encoded; nil/empty leaves the column empty).
func (s *Store) FinalizeEvent(id string, endTS int64, score float64, cells []int) error {
	cellsJSON := ""
	if len(cells) > 0 {
		if b, err := json.Marshal(cells); err == nil {
			cellsJSON = string(b)
		}
	}
	return affected(s.db.Exec(`UPDATE events SET end_ts = ?, score = ?, motion_cells = ? WHERE id = ?`,
		endTS, score, cellsJSON, id))
}

// SetEventThumb records the thumbnail path for an event.
func (s *Store) SetEventThumb(id, thumbPath string) error {
	return affected(s.db.Exec(`UPDATE events SET thumb_path = ? WHERE id = ?`, thumbPath, id))
}

// EventByID fetches an event.
func (s *Store) EventByID(id string) (*Event, error) {
	return scanEvent(s.db.QueryRow(eventSelect+` WHERE id = ?`, id))
}

// ListEvents returns events for a camera overlapping a time range.
func (s *Store) ListEvents(cameraID string, fromMs, toMs int64) ([]*Event, error) {
	rows, err := s.db.Query(eventSelect+
		` WHERE camera_id = ? AND start_ts < ? AND (end_ts IS NULL OR end_ts > ?)
          ORDER BY start_ts ASC`, cameraID, toMs, fromMs)
	if err != nil {
		return nil, err
	}
	return collectEvents(rows)
}

// ListEventsAllScoped returns recent events (newest first) within [fromMs,toMs],
// capped at limit. cameraIDs restricts the result in SQL: nil means all cameras
// (admin), a non-nil slice restricts to those cameras, and an empty slice yields
// no rows. Filtering in the query — rather than over-fetching and dropping rows
// in the handler — makes a viewer's N-row request scan at most N rows.
//
// Ordering is by seq DESC (not start_ts): seq is unique and strictly increasing,
// so it is a stable keyset key, whereas start_ts can tie. seq is assigned in
// start order for live events and was backfilled in (start_ts,id) order, so the
// visible newest-first order is unchanged. beforeSeq>0 selects the next keyset
// page (rows with seq < beforeSeq); 0 starts from the newest.
func (s *Store) ListEventsAllScoped(fromMs, toMs, beforeSeq int64, limit int, cameraIDs []string) ([]*Event, error) {
	q := eventSelect + ` WHERE start_ts >= ? AND start_ts <= ?`
	args := []any{fromMs, toMs}
	if beforeSeq > 0 {
		q += ` AND seq < ?`
		args = append(args, beforeSeq)
	}
	if cameraIDs != nil {
		if len(cameraIDs) == 0 {
			return []*Event{}, nil
		}
		ph := strings.TrimSuffix(strings.Repeat("?,", len(cameraIDs)), ",")
		q += ` AND camera_id IN (` + ph + `)`
		for _, id := range cameraIDs {
			args = append(args, id)
		}
	}
	q += ` ORDER BY seq DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	return collectEvents(rows)
}

// EventsInRange returns events overlapping [fromMs, toMs]. Empty cameraID = all.
func (s *Store) EventsInRange(cameraID string, fromMs, toMs int64) ([]*Event, error) {
	q := eventSelect + ` WHERE start_ts < ? AND (end_ts IS NULL OR end_ts > ?)`
	args := []any{toMs, fromMs}
	if cameraID != "" {
		q = eventSelect + ` WHERE camera_id = ? AND start_ts < ? AND (end_ts IS NULL OR end_ts > ?)`
		args = []any{cameraID, toMs, fromMs}
	}
	rows, err := s.db.Query(q+` ORDER BY start_ts ASC`, args...)
	if err != nil {
		return nil, err
	}
	return collectEvents(rows)
}

// EventsSinceSeq returns events with seq > afterSeq in ascending seq order,
// camera-scoped as in ListEventsAllScoped (nil = all). Backs change-feed resume:
// a reconnecting client replays durable messages it missed straight from the DB.
func (s *Store) EventsSinceSeq(afterSeq int64, limit int, cameraIDs []string) ([]*Event, error) {
	q := eventSelect + ` WHERE seq > ?`
	args := []any{afterSeq}
	if cameraIDs != nil {
		if len(cameraIDs) == 0 {
			return []*Event{}, nil
		}
		ph := strings.TrimSuffix(strings.Repeat("?,", len(cameraIDs)), ",")
		q += ` AND camera_id IN (` + ph + `)`
		for _, id := range cameraIDs {
			args = append(args, id)
		}
	}
	q += ` ORDER BY seq ASC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	return collectEvents(rows)
}

// OldestEventSeq returns the smallest seq still present (0 if no events). The
// change feed sends a `reset` when a client's cursor predates this (its missed
// events have been pruned), telling it to refetch rather than resume.
func (s *Store) OldestEventSeq() (int64, error) {
	var seq sql.NullInt64
	err := s.db.QueryRow(`SELECT MIN(seq) FROM events`).Scan(&seq)
	if err != nil {
		return 0, err
	}
	return seq.Int64, nil
}

// DeleteEvent removes an event row by id.
func (s *Store) DeleteEvent(id string) error {
	_, err := s.db.Exec(`DELETE FROM events WHERE id = ?`, id)
	return err
}

// FinalizeOrphanedEvents closes any events left ongoing by a previous run, so
// they don't render as perpetually ongoing. Called on detector startup.
func (s *Store) FinalizeOrphanedEvents() error {
	_, err := s.db.Exec(`UPDATE events SET end_ts = start_ts + 10000 WHERE end_ts IS NULL`)
	return err
}

// CountEventsSince counts events that started at/after ts.
func (s *Store) CountEventsSince(ts int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE start_ts >= ?`, ts).Scan(&n)
	return n, err
}

const eventSelect = `SELECT id, camera_id, type, start_ts, end_ts, thumb_path, snapshot_path, score, reviewed, saved, top_label, object_summary, motion_cells, seq FROM events`

func scanEvent(row rowScanner) (*Event, error) {
	var e Event
	var end, seq sql.NullInt64
	var reviewed, saved int
	var cells string
	err := row.Scan(&e.ID, &e.CameraID, &e.Type, &e.StartTS, &end, &e.ThumbPath, &e.SnapshotPath,
		&e.Score, &reviewed, &saved, &e.TopLabel, &e.ObjectSummary, &cells, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.EndTS = end.Int64
	e.Seq = seq.Int64
	e.Reviewed = reviewed != 0
	e.Saved = saved != 0
	if cells != "" {
		_ = json.Unmarshal([]byte(cells), &e.MotionCells)
	}
	return &e, nil
}

func collectEvents(rows *sql.Rows) ([]*Event, error) {
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
