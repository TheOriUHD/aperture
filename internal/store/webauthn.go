package store

import "time"

// WebAuthnCred is a stored passkey credential.
type WebAuthnCred struct {
	ID        string `json:"id"`
	UserID    string `json:"-"`
	Name      string `json:"name"`
	CredID    string `json:"-"`
	CredJSON  string `json:"-"`
	CreatedAt int64  `json:"createdAt"`
	LastUsed  int64  `json:"lastUsed"`
}

// AddWebAuthnCredential stores a new passkey for a user.
func (s *Store) AddWebAuthnCredential(userID, name, credID, credJSON string) error {
	_, err := s.db.Exec(
		`INSERT INTO webauthn_credentials (id, user_id, name, cred_id, cred_json, created_at)
         VALUES (?, ?, ?, ?, ?, ?)`,
		NewID(), userID, name, credID, credJSON, time.Now().Unix())
	return err
}

// ListWebAuthnCredentials returns a user's passkeys, newest first.
func (s *Store) ListWebAuthnCredentials(userID string) ([]WebAuthnCred, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, name, cred_id, cred_json, created_at, last_used
         FROM webauthn_credentials WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebAuthnCred
	for rows.Next() {
		var c WebAuthnCred
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.CredID, &c.CredJSON, &c.CreatedAt, &c.LastUsed); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountWebAuthnCredentials returns how many passkeys a user has.
func (s *Store) CountWebAuthnCredentials(userID string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM webauthn_credentials WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// UpdateWebAuthnCredential refreshes the stored credential JSON (sign count) and
// stamps last-used.
func (s *Store) UpdateWebAuthnCredential(credID, credJSON string) error {
	return affected(s.db.Exec(
		`UPDATE webauthn_credentials SET cred_json = ?, last_used = ? WHERE cred_id = ?`,
		credJSON, time.Now().Unix(), credID))
}

// DeleteWebAuthnCredential removes a passkey owned by the user.
func (s *Store) DeleteWebAuthnCredential(id, userID string) error {
	return affected(s.db.Exec(
		`DELETE FROM webauthn_credentials WHERE id = ? AND user_id = ?`, id, userID))
}
