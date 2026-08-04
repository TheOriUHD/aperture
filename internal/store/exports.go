package store

import (
	"database/sql"
	"strings"
)

// ExportJob is a queued/processing/finished clip export. A job covers one camera
// (legacy: CameraID) or several (CameraIDs) rendered per Mode.
type ExportJob struct {
	ID         string   `json:"id"`
	UserID     string   `json:"userId"`
	CameraID   string   `json:"cameraId"`
	CameraName string   `json:"cameraName"`
	CameraIDs  []string `json:"cameraIds"`
	Mode       string   `json:"mode"` // single | zip | composite
	FromTS     int64    `json:"fromTs"`
	ToTS       int64    `json:"toTs"`
	BurnIn     bool     `json:"burnIn"`
	Audio      bool     `json:"audio"`
	Speed      int      `json:"speed"` // 0/1 = realtime; N = timelapse N× (forces transcode)
	Status     string   `json:"status"` // queued | running | done | error
	Progress   float64  `json:"progress"`
	OutputPath string   `json:"-"`
	SizeBytes  int64    `json:"sizeBytes"`
	Error      string   `json:"error"`
	CreatedAt  int64    `json:"createdAt"`
	FinishedAt int64    `json:"finishedAt"`
}

// Cameras returns the effective camera-id list (CameraIDs, or [CameraID]).
func (j *ExportJob) Cameras() []string {
	if len(j.CameraIDs) > 0 {
		return j.CameraIDs
	}
	if j.CameraID != "" {
		return []string{j.CameraID}
	}
	return nil
}

const exportSelect = `SELECT id, user_id, camera_id, camera_name, cameras, mode, from_ts, to_ts, burn_in, audio, speed,
    status, progress, output_path, size_bytes, error, created_at, finished_at FROM export_jobs`

func scanExport(row rowScanner) (*ExportJob, error) {
	var j ExportJob
	var burn, audio int
	var cameras string
	if err := row.Scan(&j.ID, &j.UserID, &j.CameraID, &j.CameraName, &cameras, &j.Mode, &j.FromTS, &j.ToTS,
		&burn, &audio, &j.Speed, &j.Status, &j.Progress, &j.OutputPath, &j.SizeBytes, &j.Error,
		&j.CreatedAt, &j.FinishedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	j.BurnIn, j.Audio = burn != 0, audio != 0
	if cameras != "" {
		j.CameraIDs = strings.Split(cameras, ",")
	}
	return &j, nil
}

func (s *Store) CreateExport(j *ExportJob) error {
	if j.ID == "" {
		j.ID = NewID()
	}
	if j.Mode == "" {
		j.Mode = "single"
	}
	_, err := s.db.Exec(
		`INSERT INTO export_jobs (id, user_id, camera_id, camera_name, cameras, mode, from_ts, to_ts, burn_in, audio, speed, status, progress, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.ID, j.UserID, j.CameraID, j.CameraName, strings.Join(j.CameraIDs, ","), j.Mode, j.FromTS, j.ToTS,
		boolInt(j.BurnIn), boolInt(j.Audio), j.Speed, j.Status, j.Progress, j.CreatedAt)
	return err
}

func (s *Store) GetExport(id string) (*ExportJob, error) {
	return scanExport(s.db.QueryRow(exportSelect+` WHERE id = ?`, id))
}

func (s *Store) ListExportsByUser(userID string, limit int) ([]*ExportJob, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(exportSelect+` WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	return collectExports(rows)
}

// PendingExports returns jobs left queued/running (e.g. across a restart).
func (s *Store) PendingExports() ([]*ExportJob, error) {
	rows, err := s.db.Query(exportSelect + ` WHERE status IN ('queued','running') ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	return collectExports(rows)
}

// ExportsBefore returns finished jobs created before ts (for TTL cleanup).
func (s *Store) ExportsBefore(ts int64) ([]*ExportJob, error) {
	rows, err := s.db.Query(exportSelect+` WHERE created_at < ?`, ts)
	if err != nil {
		return nil, err
	}
	return collectExports(rows)
}

func (s *Store) SetExportProgress(id string, progress float64) error {
	_, err := s.db.Exec(`UPDATE export_jobs SET progress = ?, status = 'running' WHERE id = ?`, progress, id)
	return err
}

func (s *Store) FinishExport(id, outputPath string, size int64, finishedAt int64) error {
	_, err := s.db.Exec(
		`UPDATE export_jobs SET status='done', progress=1, output_path=?, size_bytes=?, finished_at=? WHERE id=?`,
		outputPath, size, finishedAt, id)
	return err
}

func (s *Store) FailExport(id, msg string, finishedAt int64) error {
	_, err := s.db.Exec(`UPDATE export_jobs SET status='error', error=?, finished_at=? WHERE id=?`, msg, finishedAt, id)
	return err
}

func (s *Store) DeleteExport(id string) error {
	_, err := s.db.Exec(`DELETE FROM export_jobs WHERE id = ?`, id)
	return err
}

func collectExports(rows *sql.Rows) ([]*ExportJob, error) {
	defer rows.Close()
	var out []*ExportJob
	for rows.Next() {
		j, err := scanExport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
