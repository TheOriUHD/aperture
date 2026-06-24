package store

import (
	"database/sql"
	"errors"
	"time"
)

// GetPrefs returns a user's preferences JSON document ("{}" if none).
func (s *Store) GetPrefs(userID string) (string, error) {
	var p string
	err := s.db.QueryRow(`SELECT prefs FROM user_preferences WHERE user_id = ?`, userID).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) || p == "" {
		return "{}", nil
	}
	if err != nil {
		return "{}", err
	}
	return p, nil
}

// SetPrefs upserts a user's preferences JSON document.
func (s *Store) SetPrefs(userID, prefs string) error {
	_, err := s.db.Exec(
		`INSERT INTO user_preferences (user_id, prefs, updated_at) VALUES (?, ?, ?)
         ON CONFLICT(user_id) DO UPDATE SET prefs = excluded.prefs, updated_at = excluded.updated_at`,
		userID, prefs, time.Now().Unix())
	return err
}
