package store

import (
	"database/sql"
	"errors"
	"time"
)

// CreateCamera inserts a new camera and returns its id.
func (s *Store) CreateCamera(c *Camera) error {
	if c.ID == "" {
		c.ID = NewID()
	}
	now := time.Now().Unix()
	if c.CreatedAt == 0 {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if c.RecordMode == "" {
		c.RecordMode = ModeContinuous
	}
	if c.RetentionDays == 0 {
		c.RetentionDays = 7
	}
	if c.MotionThreshold == 0 {
		c.MotionThreshold = 0.02
	}
	_, err := s.db.Exec(
		`INSERT INTO cameras
         (id, name, rtsp_main_url, rtsp_sub_url, enabled, record_mode, retention_days,
          storage_quota_bytes, motion_threshold, motion_zones_json, fps, sort_order, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.RTSPMainURL, c.RTSPSubURL, boolInt(c.Enabled), c.RecordMode, c.RetentionDays,
		c.StorageQuotaBytes, c.MotionThreshold, c.MotionZonesJSON, c.FPS, c.SortOrder, c.CreatedAt, c.UpdatedAt,
	)
	return err
}

// GetCamera fetches a camera by id.
func (s *Store) GetCamera(id string) (*Camera, error) {
	return s.scanCamera(s.db.QueryRow(cameraSelect+` WHERE id = ?`, id))
}

// ListCameras returns all cameras ordered by sort order then name.
func (s *Store) ListCameras() ([]*Camera, error) {
	rows, err := s.db.Query(cameraSelect + ` ORDER BY sort_order ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Camera
	for rows.Next() {
		c, err := s.scanCamera(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateCamera persists mutable fields of a camera.
func (s *Store) UpdateCamera(c *Camera) error {
	c.UpdatedAt = time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE cameras SET
            name = ?, rtsp_main_url = ?, rtsp_sub_url = ?, enabled = ?, record_mode = ?,
            retention_days = ?, storage_quota_bytes = ?, motion_threshold = ?, motion_zones_json = ?,
            fps = ?, sort_order = ?, updated_at = ?
         WHERE id = ?`,
		c.Name, c.RTSPMainURL, c.RTSPSubURL, boolInt(c.Enabled), c.RecordMode,
		c.RetentionDays, c.StorageQuotaBytes, c.MotionThreshold, c.MotionZonesJSON,
		c.FPS, c.SortOrder, c.UpdatedAt, c.ID,
	)
	return affected(res, err)
}

// DeleteCamera removes a camera (cascades to segments/events/acl).
func (s *Store) DeleteCamera(id string) error {
	return affected(s.db.Exec(`DELETE FROM cameras WHERE id = ?`, id))
}

const cameraSelect = `SELECT id, name, rtsp_main_url, rtsp_sub_url, enabled, record_mode,
    retention_days, storage_quota_bytes, motion_threshold, motion_zones_json, fps,
    sort_order, created_at, updated_at FROM cameras`

func (s *Store) scanCamera(row rowScanner) (*Camera, error) {
	var c Camera
	var enabled int
	err := row.Scan(&c.ID, &c.Name, &c.RTSPMainURL, &c.RTSPSubURL, &enabled, &c.RecordMode,
		&c.RetentionDays, &c.StorageQuotaBytes, &c.MotionThreshold, &c.MotionZonesJSON, &c.FPS,
		&c.SortOrder, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Enabled = enabled != 0
	return &c, nil
}
