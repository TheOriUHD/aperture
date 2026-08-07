package store

import (
	"database/sql"
	"errors"
	"time"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("not found")

// CountUsers returns the number of accounts (used to detect first-run).
func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser inserts a new account.
func (s *Store) CreateUser(u *User) error {
	if u.ID == "" {
		u.ID = NewID()
	}
	if u.CreatedAt == 0 {
		u.CreatedAt = time.Now().Unix()
	}
	if u.Role == "" {
		u.Role = RoleViewer
	}
	_, err := s.db.Exec(
		`INSERT INTO users (id, email, name, password_hash, role, oidc_sub, disabled, created_at, must_change_password)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.Name, nullStr(u.PasswordHash), u.Role, u.OIDCSub, boolInt(u.Disabled), u.CreatedAt,
		boolInt(u.MustChangePassword),
	)
	return err
}

// SetMustChangePassword flags (or clears) the forced first-login reset.
func (s *Store) SetMustChangePassword(id string, must bool) error {
	return affected(s.db.Exec(`UPDATE users SET must_change_password = ? WHERE id = ?`, boolInt(must), id))
}

// SetUserTOTP stores a user's TOTP secret/enabled state + recovery codes.
func (s *Store) SetUserTOTP(id, secret string, enabled bool, recovery string) error {
	return affected(s.db.Exec(
		`UPDATE users SET totp_secret = ?, totp_enabled = ?, totp_recovery = ? WHERE id = ?`,
		secret, boolInt(enabled), recovery, id))
}

// GetUserByEmail looks up an account by email (case-sensitive).
func (s *Store) GetUserByEmail(email string) (*User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, email, name, password_hash, role, oidc_sub, disabled, created_at,
                must_change_password, totp_secret, totp_enabled, totp_recovery
         FROM users WHERE email = ?`, email))
}

// GetUserByOIDCSub looks up an account by its linked OIDC subject.
func (s *Store) GetUserByOIDCSub(sub string) (*User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, email, name, password_hash, role, oidc_sub, disabled, created_at,
                must_change_password, totp_secret, totp_enabled, totp_recovery
         FROM users WHERE oidc_sub = ?`, sub))
}

// SetUserOIDC links an account to an OIDC subject.
func (s *Store) SetUserOIDC(id, sub string) error {
	return affected(s.db.Exec(`UPDATE users SET oidc_sub = ? WHERE id = ?`, sub, id))
}

// GetUserByID looks up an account by id.
func (s *Store) GetUserByID(id string) (*User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, email, name, password_hash, role, oidc_sub, disabled, created_at,
                must_change_password, totp_secret, totp_enabled, totp_recovery
         FROM users WHERE id = ?`, id))
}

// FirstAdminID returns the earliest-created admin's id. Used to own an extension
// (and its scoped token) when the installer is the synthetic CLI superuser, which
// has no users row of its own — so a token minted for "cli" would violate the
// api_tokens.user_id foreign key and the backend would never spawn.
func (s *Store) FirstAdminID() (string, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM users WHERE role = ? ORDER BY created_at ASC LIMIT 1`, RoleAdmin).Scan(&id)
	return id, err
}

// ListUsers returns all accounts ordered by creation time.
func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(
		`SELECT id, email, name, password_hash, role, oidc_sub, disabled, created_at,
                must_change_password, totp_secret, totp_enabled, totp_recovery
         FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := s.scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetUserPassword updates the bcrypt hash for an account.
func (s *Store) SetUserPassword(id, hash string) error {
	res, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return affected(res, err)
}

// UpdateUser updates an account's mutable profile fields.
func (s *Store) UpdateUser(id, name, role string, disabled bool) error {
	res, err := s.db.Exec(`UPDATE users SET name = ?, role = ?, disabled = ? WHERE id = ?`,
		name, role, boolInt(disabled), id)
	return affected(res, err)
}

// DeleteUser removes an account (and its camera ACL rows via cascade).
func (s *Store) DeleteUser(id string) error {
	return affected(s.db.Exec(`DELETE FROM users WHERE id = ?`, id))
}

// GetUserCameraIDs returns the camera ids a user is scoped to.
func (s *Store) GetUserCameraIDs(userID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT camera_id FROM user_camera_acl WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetUserCameras replaces a user's camera ACL with the given camera ids.
func (s *Store) SetUserCameras(userID string, cameraIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM user_camera_acl WHERE user_id = ?`, userID); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, cid := range cameraIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO user_camera_acl (user_id, camera_id) VALUES (?, ?)`,
			userID, cid); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanUser(row rowScanner) (*User, error) {
	var u User
	var pw sql.NullString
	var oidc sql.NullString
	var disabled, mustChange, totpEnabled int
	err := row.Scan(&u.ID, &u.Email, &u.Name, &pw, &u.Role, &oidc, &disabled, &u.CreatedAt,
		&mustChange, &u.TOTPSecret, &totpEnabled, &u.TOTPRecovery)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.PasswordHash = pw.String
	if oidc.Valid {
		v := oidc.String
		u.OIDCSub = &v
	}
	u.Disabled = disabled != 0
	u.MustChangePassword = mustChange != 0
	u.TOTPEnabled = totpEnabled != 0
	return &u, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
