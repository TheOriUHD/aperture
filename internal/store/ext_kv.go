package store

import (
	"database/sql"
	"errors"
	"time"
)

// Per-extension private KV (the manifest `storage: true` permission). Namespaced by
// ext_id; the API layer enforces that a token only ever touches its own namespace.
// A per-extension row-count and byte quota is enforced here so a runaway extension
// can't bloat the single shared SQLite database.

const (
	// ExtKVMaxRows / ExtKVMaxBytes bound one extension's private store. Generous for
	// config/state, far below anything that would threaten the DB.
	ExtKVMaxRows    = 1000
	ExtKVMaxBytes   = 1 << 20 // 1 MiB of values per extension
	ExtKVMaxValueSz = 64 << 10
)

// ErrExtKVQuota is returned when a set would exceed the extension's row/byte quota.
var ErrExtKVQuota = errors.New("extension storage quota exceeded")

// ExtKVGet returns (value, true) or ("", false) when the key is absent.
func (s *Store) ExtKVGet(extID, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM ext_kv WHERE ext_id = ? AND key = ?`, extID, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// ExtKVKeys lists an extension's keys (sorted), for enumeration.
func (s *Store) ExtKVKeys(extID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT key FROM ext_kv WHERE ext_id = ? ORDER BY key`, extID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ExtKVSet upserts a value, enforcing the per-extension row + byte quota. The quota
// check and the write share the single connection, so the count can't race.
func (s *Store) ExtKVSet(extID, key, value string) error {
	if len(value) > ExtKVMaxValueSz {
		return ErrExtKVQuota
	}
	// Current footprint excluding this key (an upsert replaces it).
	var rowsN, bytesN sql.NullInt64
	if err := s.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(LENGTH(value)), 0) FROM ext_kv WHERE ext_id = ? AND key <> ?`,
		extID, key).Scan(&rowsN, &bytesN); err != nil {
		return err
	}
	if rowsN.Int64+1 > ExtKVMaxRows || bytesN.Int64+int64(len(value)) > ExtKVMaxBytes {
		return ErrExtKVQuota
	}
	_, err := s.db.Exec(
		`INSERT INTO ext_kv (ext_id, key, value, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(ext_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		extID, key, value, time.Now().UnixMilli())
	return err
}

// ExtKVDelete removes one key (no error if absent).
func (s *Store) ExtKVDelete(extID, key string) error {
	_, err := s.db.Exec(`DELETE FROM ext_kv WHERE ext_id = ? AND key = ?`, extID, key)
	return err
}

// ExtKVDeleteAll drops an extension's entire namespace (called on uninstall).
func (s *Store) ExtKVDeleteAll(extID string) error {
	_, err := s.db.Exec(`DELETE FROM ext_kv WHERE ext_id = ?`, extID)
	return err
}
