package store

import (
	"database/sql"
	"errors"
	"strconv"
)

// GetSetting returns the value for key, or ("", ErrNotFound) if unset.
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

// SetSetting upserts a key/value pair.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
         ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// GetSettingInt64 returns a numeric setting, or 0 if unset/invalid.
func (s *Store) GetSettingInt64(key string) int64 {
	v, err := s.GetSetting(key)
	if err != nil || v == "" {
		return 0
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

// RecordingsPathOverride returns the operator-configured recordings directory,
// or "" to use the deploy default. New footage is written here; existing
// segments keep their stored absolute paths.
func (s *Store) RecordingsPathOverride() string {
	v, _ := s.GetSetting("recordings_path")
	return v
}

// GetSettingBool reads a boolean setting; anything other than "1"/"true" (and a
// missing key) is false.
func (s *Store) GetSettingBool(key string) bool {
	v, _ := s.GetSetting(key)
	return v == "1" || v == "true"
}

// SetSettingBool stores a boolean setting as "1"/"0".
func (s *Store) SetSettingBool(key string, v bool) error {
	if v {
		return s.SetSetting(key, "1")
	}
	return s.SetSetting(key, "0")
}
