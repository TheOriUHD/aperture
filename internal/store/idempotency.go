package store

import "time"

// GetIdempotent returns the stored response for an idempotency key if one exists
// and has not expired.
func (s *Store) GetIdempotent(key string) (status int, response string, ok bool) {
	var st int
	var resp string
	var exp int64
	if err := s.db.QueryRow(
		`SELECT status_code, response, expires_at FROM idempotency_keys WHERE key = ?`, key,
	).Scan(&st, &resp, &exp); err != nil {
		return 0, "", false
	}
	if time.Now().UnixMilli() > exp {
		return 0, "", false
	}
	return st, resp, true
}

// SaveIdempotent records a response against an idempotency key with a TTL.
func (s *Store) SaveIdempotent(key, method, pathHash string, status int, response string, ttl time.Duration) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO idempotency_keys (key, method, path_hash, status_code, response, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		key, method, pathHash, status, response, now, now+ttl.Milliseconds())
	return err
}

// PurgeExpiredIdempotency deletes keys past their TTL (housekeeping).
func (s *Store) PurgeExpiredIdempotency() error {
	_, err := s.db.Exec(`DELETE FROM idempotency_keys WHERE expires_at < ?`, time.Now().UnixMilli())
	return err
}
