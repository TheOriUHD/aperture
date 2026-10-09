package store

import (
	"database/sql"
	"errors"
)

// Session is a signed-in browser. The cookie holds a random token; only its hash
// is stored here.
type Session struct {
	ID           string `json:"id"`
	UserID       string `json:"-"`
	TokenHash    string `json:"-"`
	DeviceID     string `json:"-"`
	CreatedAt    int64  `json:"createdAt"`
	LastSeenAt   int64  `json:"lastSeenAt"`
	ExpiresAt    int64  `json:"expiresAt"`
	MaxExpiresAt int64  `json:"-"`
	IP           string `json:"ip"`
	UA           string `json:"ua"`
}

// TrustedDevice is a browser that completed a second factor and may skip it on
// its next password sign-in.
type TrustedDevice struct {
	ID         string
	UserID     string
	TokenHash  string
	CreatedAt  int64
	LastUsedAt int64
	ExpiresAt  int64
	IP         string
	UA         string
}

const sessionCols = `s.id, s.user_id, s.token_hash, s.device_id, s.created_at, s.last_seen_at,
        s.expires_at, s.max_expires_at, s.ip, s.ua`

func scanSession(row rowScanner) (*Session, error) {
	var x Session
	err := row.Scan(&x.ID, &x.UserID, &x.TokenHash, &x.DeviceID, &x.CreatedAt, &x.LastSeenAt,
		&x.ExpiresAt, &x.MaxExpiresAt, &x.IP, &x.UA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &x, err
}

// CreateSession stores a new session (assigning an id when empty).
func (s *Store) CreateSession(x *Session) error {
	if x.ID == "" {
		x.ID = NewID()
	}
	_, err := s.db.Exec(`INSERT INTO sessions
        (id, user_id, token_hash, device_id, created_at, last_seen_at, expires_at, max_expires_at, ip, ua)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		x.ID, x.UserID, x.TokenHash, x.DeviceID, x.CreatedAt, x.LastSeenAt, x.ExpiresAt, x.MaxExpiresAt, x.IP, x.UA)
	return err
}

// SessionByTokenHash returns a session together with its user, in one query: it
// runs on every cookie-authenticated request.
func (s *Store) SessionByTokenHash(hash string) (*Session, *User, error) {
	var x Session
	var u User
	var pw, oidc sql.NullString
	var disabled, mustChange, totpEnabled int
	err := s.db.QueryRow(`SELECT `+sessionCols+`,
        u.id, u.email, u.name, u.password_hash, u.role, u.oidc_sub, u.disabled, u.created_at,
        u.must_change_password, u.totp_secret, u.totp_enabled, u.totp_recovery
        FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ?`, hash).Scan(
		&x.ID, &x.UserID, &x.TokenHash, &x.DeviceID, &x.CreatedAt, &x.LastSeenAt,
		&x.ExpiresAt, &x.MaxExpiresAt, &x.IP, &x.UA,
		&u.ID, &u.Email, &u.Name, &pw, &u.Role, &oidc, &disabled, &u.CreatedAt,
		&mustChange, &u.TOTPSecret, &totpEnabled, &u.TOTPRecovery)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	u.PasswordHash = pw.String
	if oidc.Valid {
		v := oidc.String
		u.OIDCSub = &v
	}
	u.Disabled = disabled != 0
	u.MustChangePassword = mustChange != 0
	u.TOTPEnabled = totpEnabled != 0
	return &x, &u, nil
}

// TouchSession records use of a session and slides its expiry.
func (s *Store) TouchSession(id string, lastSeen, expires int64) error {
	_, err := s.db.Exec(`UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`, lastSeen, expires, id)
	return err
}

// ListSessions returns a user's unexpired sessions, most recently used first.
func (s *Store) ListSessions(userID string, now int64) ([]*Session, error) {
	rows, err := s.db.Query(`SELECT `+sessionCols+` FROM sessions s
        WHERE s.user_id = ? AND s.expires_at > ? ORDER BY s.last_seen_at DESC`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// GetSession returns one session by id.
func (s *Store) GetSession(id string) (*Session, error) {
	return scanSession(s.db.QueryRow(`SELECT `+sessionCols+` FROM sessions s WHERE s.id = ?`, id))
}

// DeleteSession ends one session.
func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteUserSessions ends every session of a user except keepID ("" = all) and
// reports how many ended.
func (s *Store) DeleteUserSessions(userID, keepID string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ? AND id != ?`, userID, keepID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PurgeExpiredSessions drops sessions and trusted devices past their expiry.
func (s *Store) PurgeExpiredSessions(now int64) error {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ? OR max_expires_at <= ?`, now, now); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM trusted_devices WHERE expires_at <= ?`, now)
	return err
}

// CreateTrustedDevice stores a trusted device (assigning an id when empty).
func (s *Store) CreateTrustedDevice(d *TrustedDevice) error {
	if d.ID == "" {
		d.ID = NewID()
	}
	_, err := s.db.Exec(`INSERT INTO trusted_devices
        (id, user_id, token_hash, created_at, last_used_at, expires_at, ip, ua)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.UserID, d.TokenHash, d.CreatedAt, d.LastUsedAt, d.ExpiresAt, d.IP, d.UA)
	return err
}

// TrustedDeviceByTokenHash returns the trusted device holding a device token.
func (s *Store) TrustedDeviceByTokenHash(hash string) (*TrustedDevice, error) {
	var d TrustedDevice
	err := s.db.QueryRow(`SELECT id, user_id, token_hash, created_at, last_used_at, expires_at, ip, ua
        FROM trusted_devices WHERE token_hash = ?`, hash).Scan(
		&d.ID, &d.UserID, &d.TokenHash, &d.CreatedAt, &d.LastUsedAt, &d.ExpiresAt, &d.IP, &d.UA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GetTrustedDevice returns one trusted device by id.
func (s *Store) GetTrustedDevice(id string) (*TrustedDevice, error) {
	var d TrustedDevice
	err := s.db.QueryRow(`SELECT id, user_id, token_hash, created_at, last_used_at, expires_at, ip, ua
        FROM trusted_devices WHERE id = ?`, id).Scan(
		&d.ID, &d.UserID, &d.TokenHash, &d.CreatedAt, &d.LastUsedAt, &d.ExpiresAt, &d.IP, &d.UA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// TouchTrustedDevice records use of a trusted device and slides its expiry.
func (s *Store) TouchTrustedDevice(id string, lastUsed, expires int64) error {
	_, err := s.db.Exec(`UPDATE trusted_devices SET last_used_at = ?, expires_at = ? WHERE id = ?`, lastUsed, expires, id)
	return err
}

// DeleteTrustedDevice forgets one trusted device.
func (s *Store) DeleteTrustedDevice(id string) error {
	_, err := s.db.Exec(`DELETE FROM trusted_devices WHERE id = ?`, id)
	return err
}

// DeleteUserTrustedDevices forgets every trusted device of a user except keepID
// ("" = all) and reports how many it forgot.
func (s *Store) DeleteUserTrustedDevices(userID, keepID string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM trusted_devices WHERE user_id = ? AND id != ?`, userID, keepID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountTrustedDevices counts a user's unexpired trusted devices.
func (s *Store) CountTrustedDevices(userID string, now int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM trusted_devices WHERE user_id = ? AND expires_at > ?`, userID, now).Scan(&n)
	return n, err
}
