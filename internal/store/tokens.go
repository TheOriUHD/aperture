package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// APIToken is a revocable Bearer credential for automation/integrations. It is
// bound to a user, so it inherits that user's role and camera ACLs — a viewer's
// token can only read what the viewer can see.
type APIToken struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	Name      string `json:"name"`
	Prefix    string `json:"prefix"`
	CreatedAt int64  `json:"createdAt"`
	LastUsed  int64  `json:"lastUsed"`
	ExpiresAt int64  `json:"expiresAt"`
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// APITokenPrefix marks a plaintext token so the auth layer only hits the DB for
// strings that could actually be one.
const APITokenPrefix = "apt_"

// CreateAPIToken mints a token, stores only its hash, and returns the plaintext
// exactly once. expiresAt of 0 means it never expires.
func (s *Store) CreateAPIToken(userID, name string, expiresAt int64) (*APIToken, string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", err
	}
	plain := APITokenPrefix + hex.EncodeToString(raw)
	t := &APIToken{
		ID: NewID(), UserID: userID, Name: name,
		Prefix: plain[:12], CreatedAt: time.Now().UnixMilli(), ExpiresAt: expiresAt,
	}
	_, err := s.db.Exec(
		`INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, created_at, last_used, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, 0, ?)`,
		t.ID, t.UserID, t.Name, hashToken(plain), t.Prefix, t.CreatedAt, t.ExpiresAt)
	if err != nil {
		return nil, "", err
	}
	return t, plain, nil
}

// ListAPITokens returns a user's tokens (never the secret).
func (s *Store) ListAPITokens(userID string) ([]APIToken, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, name, prefix, created_at, last_used, expires_at
		 FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &t.CreatedAt, &t.LastUsed, &t.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPIToken revokes a token (scoped to its owner).
func (s *Store) DeleteAPIToken(id, userID string) error {
	return affected(s.db.Exec(`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID))
}

// ResolveAPIToken returns the owning user id for a valid, unexpired token and
// records its use. ok is false for unknown or expired tokens.
func (s *Store) ResolveAPIToken(plain string) (userID string, ok bool) {
	h := hashToken(plain)
	var uid string
	var exp int64
	if err := s.db.QueryRow(`SELECT user_id, expires_at FROM api_tokens WHERE token_hash = ?`, h).Scan(&uid, &exp); err != nil {
		return "", false
	}
	if exp != 0 && time.Now().UnixMilli() > exp {
		return "", false
	}
	_, _ = s.db.Exec(`UPDATE api_tokens SET last_used = ? WHERE token_hash = ?`, time.Now().UnixMilli(), h)
	return uid, true
}
