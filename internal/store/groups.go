package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// CameraGroup is an admin-defined named set of cameras (membership is a list of
// camera ids; a camera may be in several groups). The "All Cameras" group is
// virtual and synthesised by the API, not stored.
type CameraGroup struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	CameraIDs []string `json:"cameraIds"`
	SortOrder int      `json:"sortOrder"`
	CreatedAt int64    `json:"createdAt"`
	UpdatedAt int64    `json:"updatedAt"`
}

const groupSelect = `SELECT id, name, cameras_json, sort_order, created_at, updated_at FROM camera_groups`

func scanGroup(row rowScanner) (*CameraGroup, error) {
	var g CameraGroup
	var cams string
	if err := row.Scan(&g.ID, &g.Name, &cams, &g.SortOrder, &g.CreatedAt, &g.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	g.CameraIDs = []string{}
	if cams != "" {
		_ = json.Unmarshal([]byte(cams), &g.CameraIDs)
	}
	return &g, nil
}

// ListCameraGroups returns all custom groups, ordered.
func (s *Store) ListCameraGroups() ([]*CameraGroup, error) {
	rows, err := s.db.Query(groupSelect + ` ORDER BY sort_order ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CameraGroup
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GetCameraGroup fetches one group by id.
func (s *Store) GetCameraGroup(id string) (*CameraGroup, error) {
	return scanGroup(s.db.QueryRow(groupSelect+` WHERE id = ?`, id))
}

// CreateCameraGroup inserts a new group.
func (s *Store) CreateCameraGroup(g *CameraGroup) error {
	if g.ID == "" {
		g.ID = NewID()
	}
	now := time.Now().Unix()
	g.CreatedAt, g.UpdatedAt = now, now
	if g.CameraIDs == nil {
		g.CameraIDs = []string{}
	}
	b, _ := json.Marshal(g.CameraIDs)
	_, err := s.db.Exec(
		`INSERT INTO camera_groups (id, name, cameras_json, sort_order, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?)`,
		g.ID, g.Name, string(b), g.SortOrder, g.CreatedAt, g.UpdatedAt)
	return err
}

// UpdateCameraGroup persists a group's name + membership.
func (s *Store) UpdateCameraGroup(g *CameraGroup) error {
	if g.CameraIDs == nil {
		g.CameraIDs = []string{}
	}
	b, _ := json.Marshal(g.CameraIDs)
	return affected(s.db.Exec(
		`UPDATE camera_groups SET name = ?, cameras_json = ?, sort_order = ?, updated_at = ? WHERE id = ?`,
		g.Name, string(b), g.SortOrder, time.Now().Unix(), g.ID))
}

// DeleteCameraGroup removes a group.
func (s *Store) DeleteCameraGroup(id string) error {
	return affected(s.db.Exec(`DELETE FROM camera_groups WHERE id = ?`, id))
}

// SetCameraGroups sets exactly which groups a camera belongs to, updating each
// affected group's membership (the "assign from the camera side" path).
func (s *Store) SetCameraGroups(cameraID string, groupIDs []string) error {
	want := map[string]bool{}
	for _, id := range groupIDs {
		want[id] = true
	}
	groups, err := s.ListCameraGroups()
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	now := time.Now().Unix()
	for _, g := range groups {
		has := false
		next := make([]string, 0, len(g.CameraIDs))
		for _, cid := range g.CameraIDs {
			if cid == cameraID {
				has = true
				continue // drop; re-added below if wanted
			}
			next = append(next, cid)
		}
		inWant := want[g.ID]
		if inWant {
			next = append(next, cameraID)
		}
		if has == inWant {
			continue // no change for this group
		}
		b, _ := json.Marshal(next)
		if _, err := tx.Exec(`UPDATE camera_groups SET cameras_json = ?, updated_at = ? WHERE id = ?`,
			string(b), now, g.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
