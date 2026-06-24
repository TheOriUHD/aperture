package store

import "database/sql"

// Share is a scoped, expiring public link to a resource (currently a clip).
type Share struct {
	ID           string `json:"id"`
	TokenHash    string `json:"-"`
	Kind         string `json:"kind"`   // clip
	Target       string `json:"target"` // export job id
	Label        string `json:"label"`
	ExpiresAt    int64  `json:"expiresAt"` // 0 = never
	PasswordHash string `json:"-"`
	MaxViews     int    `json:"maxViews"` // 0 = unlimited
	Views        int    `json:"views"`
	CreatedBy    string `json:"createdBy"`
	Revoked      bool   `json:"revoked"`
	CreatedAt    int64  `json:"createdAt"`
}

const shareSelect = `SELECT id, token_hash, kind, target, label, expires_at, password_hash,
    max_views, views, created_by, revoked, created_at FROM shares`

func scanShare(row rowScanner) (*Share, error) {
	var sh Share
	var revoked int
	if err := row.Scan(&sh.ID, &sh.TokenHash, &sh.Kind, &sh.Target, &sh.Label, &sh.ExpiresAt,
		&sh.PasswordHash, &sh.MaxViews, &sh.Views, &sh.CreatedBy, &revoked, &sh.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sh.Revoked = revoked != 0
	return &sh, nil
}

func (s *Store) CreateShare(sh *Share) error {
	if sh.ID == "" {
		sh.ID = NewID()
	}
	_, err := s.db.Exec(
		`INSERT INTO shares (id, token_hash, kind, target, label, expires_at, password_hash, max_views, views, created_by, revoked, created_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, 0, ?)`,
		sh.ID, sh.TokenHash, sh.Kind, sh.Target, sh.Label, sh.ExpiresAt, sh.PasswordHash,
		sh.MaxViews, sh.CreatedBy, sh.CreatedAt)
	return err
}

func (s *Store) GetShare(id string) (*Share, error) {
	return scanShare(s.db.QueryRow(shareSelect+` WHERE id = ?`, id))
}

func (s *Store) GetShareByTokenHash(h string) (*Share, error) {
	return scanShare(s.db.QueryRow(shareSelect+` WHERE token_hash = ?`, h))
}

func (s *Store) ListSharesByUser(userID string) ([]*Share, error) {
	rows, err := s.db.Query(shareSelect+` WHERE created_by = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Share
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

func (s *Store) IncShareViews(id string) error {
	_, err := s.db.Exec(`UPDATE shares SET views = views + 1 WHERE id = ?`, id)
	return err
}

func (s *Store) RevokeShare(id string) error {
	return affected(s.db.Exec(`UPDATE shares SET revoked = 1 WHERE id = ?`, id))
}

func (s *Store) DeleteShare(id string) error {
	_, err := s.db.Exec(`DELETE FROM shares WHERE id = ?`, id)
	return err
}
