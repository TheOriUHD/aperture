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
	if c.PrerollMs == 0 {
		c.PrerollMs = 5000
	}
	if c.PostrollMs == 0 {
		c.PostrollMs = 5000
	}
	if c.CooldownMs == 0 {
		c.CooldownMs = 5000
	}
	_, err := s.db.Exec(
		`INSERT INTO cameras
         (id, name, rtsp_main_url, rtsp_sub_url, enabled, record_mode, retention_days,
          storage_quota_bytes, motion_threshold, motion_zones_json, motion_detection, audio_detection,
          audio_threshold, location_id, detect_method,
          temp_sensor, vendor, fps, sort_order, preroll_ms, postroll_ms, cooldown_ms, min_duration_ms, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.RTSPMainURL, c.RTSPSubURL, boolInt(c.Enabled), c.RecordMode, c.RetentionDays,
		c.StorageQuotaBytes, c.MotionThreshold, c.MotionZonesJSON, boolInt(c.MotionDetection), boolInt(c.AudioDetection),
		c.AudioThreshold, c.LocationID, c.DetectMethod,
		c.TempSensor, c.Vendor, c.FPS, c.SortOrder, c.PrerollMs, c.PostrollMs, c.CooldownMs, c.MinDurationMs, c.CreatedAt, c.UpdatedAt,
	)
	return err
}

// GetCamera fetches a camera by id.
func (s *Store) GetCamera(id string) (*Camera, error) {
	return s.scanCamera(s.db.QueryRow(cameraSelect+` WHERE id = ?`, id))
}

// ReorderCameras sets sort_order to each id's position in the list, atomically.
func (s *Store) ReorderCameras(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE cameras SET sort_order = ? WHERE id = ?`, i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
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
            motion_detection = ?, audio_detection = ?, audio_threshold = ?, location_id = ?, detect_method = ?, temp_sensor = ?, vendor = ?,
            fps = ?, sort_order = ?, preroll_ms = ?, postroll_ms = ?, cooldown_ms = ?,
            min_duration_ms = ?, updated_at = ?
         WHERE id = ?`,
		c.Name, c.RTSPMainURL, c.RTSPSubURL, boolInt(c.Enabled), c.RecordMode,
		c.RetentionDays, c.StorageQuotaBytes, c.MotionThreshold, c.MotionZonesJSON,
		boolInt(c.MotionDetection), boolInt(c.AudioDetection), c.AudioThreshold, c.LocationID, c.DetectMethod, c.TempSensor, c.Vendor,
		c.FPS, c.SortOrder, c.PrerollMs, c.PostrollMs, c.CooldownMs, c.MinDurationMs,
		c.UpdatedAt, c.ID,
	)
	return affected(res, err)
}

// DeleteCamera removes a camera (cascades to segments/events/acl).
func (s *Store) DeleteCamera(id string) error {
	return affected(s.db.Exec(`DELETE FROM cameras WHERE id = ?`, id))
}

const cameraSelect = `SELECT id, name, rtsp_main_url, rtsp_sub_url, enabled, record_mode,
    retention_days, storage_quota_bytes, motion_threshold, motion_zones_json, motion_detection,
    audio_detection, audio_threshold, location_id, detect_method, temp_sensor, vendor, fps, sort_order, preroll_ms, postroll_ms,
    cooldown_ms, min_duration_ms, created_at, updated_at FROM cameras`

func (s *Store) scanCamera(row rowScanner) (*Camera, error) {
	var c Camera
	var enabled, motionDet, audioDet int
	err := row.Scan(&c.ID, &c.Name, &c.RTSPMainURL, &c.RTSPSubURL, &enabled, &c.RecordMode,
		&c.RetentionDays, &c.StorageQuotaBytes, &c.MotionThreshold, &c.MotionZonesJSON, &motionDet,
		&audioDet, &c.AudioThreshold, &c.LocationID, &c.DetectMethod, &c.TempSensor, &c.Vendor, &c.FPS, &c.SortOrder, &c.PrerollMs, &c.PostrollMs, &c.CooldownMs, &c.MinDurationMs,
		&c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.Enabled = enabled != 0
	c.MotionDetection = motionDet != 0
	c.AudioDetection = audioDet != 0
	return &c, nil
}
