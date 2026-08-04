package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Storage location types.
const (
	LocationLocal = "local"
	LocationSMB   = "smb"
	LocationSFTP  = "sftp"
)

// DefaultLocationID is the virtual built-in location: today's recordings dir +
// storage_cap_bytes. It is never stored in storage_locations; the API synthesises
// it into list responses and every legacy segment/camera (location_id "") maps to
// it, so existing footage and the reserve wizard keep working with no migration.
const DefaultLocationID = ""

// StorageLocation is a named recording destination (local path, SMB, or SFTP).
// Secret material (share password / SSH key) is stored encrypted in a column that
// is never exposed through this struct — only HasCredential is surfaced.
type StorageLocation struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Type            string         `json:"type"` // local | smb | sftp
	Config          map[string]any `json:"config"`
	AllocationBytes int64          `json:"allocationBytes"` // local = hard reserve, network = soft cap (0 = unset)
	Enabled         bool           `json:"enabled"`
	SortOrder       int            `json:"sortOrder"`
	HasCredential   bool           `json:"hasCredential"` // secret_enc != "" (never exposes the secret itself)
	CreatedAt       int64          `json:"createdAt"`
	UpdatedAt       int64          `json:"updatedAt"`
}

// locationSelect deliberately omits secret_enc — it surfaces only whether a
// credential is present, never the ciphertext. Use LocationSecret for the value.
const locationSelect = `SELECT id, name, type, config_json, allocation_bytes, enabled, sort_order,
    (secret_enc != '') AS has_cred, created_at, updated_at FROM storage_locations`

func scanLocation(row rowScanner) (*StorageLocation, error) {
	var l StorageLocation
	var cfg string
	var enabled, hasCred int
	if err := row.Scan(&l.ID, &l.Name, &l.Type, &cfg, &l.AllocationBytes, &enabled, &l.SortOrder,
		&hasCred, &l.CreatedAt, &l.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	l.Enabled = enabled != 0
	l.HasCredential = hasCred != 0
	l.Config = map[string]any{}
	if cfg != "" {
		_ = json.Unmarshal([]byte(cfg), &l.Config)
	}
	return &l, nil
}

// ListStorageLocations returns all configured (non-default) locations, ordered.
func (s *Store) ListStorageLocations() ([]*StorageLocation, error) {
	rows, err := s.db.Query(locationSelect + ` ORDER BY sort_order ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*StorageLocation
	for rows.Next() {
		l, err := scanLocation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetStorageLocation fetches one location by id.
func (s *Store) GetStorageLocation(id string) (*StorageLocation, error) {
	return scanLocation(s.db.QueryRow(locationSelect+` WHERE id = ?`, id))
}

// CreateStorageLocation inserts a new location. The secret (if any) is set
// separately via SetLocationSecret so ciphertext never flows through this struct.
func (s *Store) CreateStorageLocation(l *StorageLocation) error {
	if l.ID == "" {
		l.ID = NewID()
	}
	if l.Type == "" {
		l.Type = LocationLocal
	}
	if l.Config == nil {
		l.Config = map[string]any{}
	}
	now := time.Now().Unix()
	l.CreatedAt, l.UpdatedAt = now, now
	cfg, _ := json.Marshal(l.Config)
	_, err := s.db.Exec(
		`INSERT INTO storage_locations
         (id, name, type, config_json, allocation_bytes, enabled, sort_order, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, l.Name, l.Type, string(cfg), l.AllocationBytes, boolInt(l.Enabled), l.SortOrder,
		l.CreatedAt, l.UpdatedAt)
	return err
}

// UpdateStorageLocation persists mutable, non-secret fields. It never touches
// secret_enc (see SetLocationSecret) so an update can't accidentally wipe creds.
func (s *Store) UpdateStorageLocation(l *StorageLocation) error {
	if l.Config == nil {
		l.Config = map[string]any{}
	}
	cfg, _ := json.Marshal(l.Config)
	l.UpdatedAt = time.Now().Unix()
	return affected(s.db.Exec(
		`UPDATE storage_locations SET name = ?, type = ?, config_json = ?, allocation_bytes = ?,
             enabled = ?, sort_order = ?, updated_at = ? WHERE id = ?`,
		l.Name, l.Type, string(cfg), l.AllocationBytes, boolInt(l.Enabled), l.SortOrder,
		l.UpdatedAt, l.ID))
}

// DeleteStorageLocation removes a location. Callers must guard against deleting a
// location that still holds segments or is a camera's target (see the API layer).
func (s *Store) DeleteStorageLocation(id string) error {
	return affected(s.db.Exec(`DELETE FROM storage_locations WHERE id = ?`, id))
}

// ReorderStorageLocations sets sort_order to match the given id order.
func (s *Store) ReorderStorageLocations(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	now := time.Now().Unix()
	for i, id := range ids {
		if _, err := tx.Exec(
			`UPDATE storage_locations SET sort_order = ?, updated_at = ? WHERE id = ?`,
			i, now, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetLocationSecret stores the (already-encrypted) credential ciphertext for a
// location. Pass "" to clear it.
func (s *Store) SetLocationSecret(id, enc string) error {
	return affected(s.db.Exec(
		`UPDATE storage_locations SET secret_enc = ?, updated_at = ? WHERE id = ?`,
		enc, time.Now().Unix(), id))
}

// LocationSecret returns the raw (still-encrypted) credential ciphertext, or ""
// if none is set. Decryption happens at the API/registry layer, not here.
func (s *Store) LocationSecret(id string) (string, error) {
	var enc string
	err := s.db.QueryRow(`SELECT secret_enc FROM storage_locations WHERE id = ?`, id).Scan(&enc)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return enc, err
}

// SetCameraLocation points a camera's future recordings at a location ("" =
// default). Existing segments keep their own location_id and are unaffected.
func (s *Store) SetCameraLocation(cameraID, locationID string) error {
	return affected(s.db.Exec(
		`UPDATE cameras SET location_id = ?, updated_at = ? WHERE id = ?`,
		locationID, time.Now().Unix(), cameraID))
}

// CamerasUsingLocation counts cameras that record to a location (delete guard).
func (s *Store) CamerasUsingLocation(locationID string) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM cameras WHERE location_id = ?`, locationID).Scan(&n)
	return n, err
}
