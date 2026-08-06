package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// APIToken is a revocable Bearer credential for automation/integrations. It is
// bound to a user and can never exceed that user's role or camera ACL; Scopes
// and CameraGrants narrow it further (empty scopes = no scopes; empty grants =
// the owner's full ACL).
type APIToken struct {
	ID           string   `json:"id"`
	UserID       string   `json:"userId"`
	Name         string   `json:"name"`
	Prefix       string   `json:"prefix"`
	Scopes       []string `json:"scopes"`
	CameraGrants []string `json:"cameraGrants"`
	CreatedAt    int64    `json:"createdAt"`
	LastUsed     int64    `json:"lastUsed"`
	ExpiresAt    int64    `json:"expiresAt"`
}

// csvJoin/csvSplit store a string slice in a single TEXT column. Scope and
// camera-id tokens never contain commas, so a plain CSV is safe.
func csvJoin(v []string) string { return strings.Join(v, ",") }

func csvSplit(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
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
func (s *Store) CreateAPIToken(userID, name string, expiresAt int64, scopes, cameraGrants []string) (*APIToken, string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", err
	}
	plain := APITokenPrefix + hex.EncodeToString(raw)
	t := &APIToken{
		ID: NewID(), UserID: userID, Name: name, Scopes: scopes, CameraGrants: cameraGrants,
		Prefix: plain[:12], CreatedAt: time.Now().UnixMilli(), ExpiresAt: expiresAt,
	}
	_, err := s.db.Exec(
		`INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, scopes, camera_grants, created_at, last_used, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		t.ID, t.UserID, t.Name, hashToken(plain), t.Prefix, csvJoin(scopes), csvJoin(cameraGrants), t.CreatedAt, t.ExpiresAt)
	if err != nil {
		return nil, "", err
	}
	return t, plain, nil
}

// ListAPITokens returns a user's tokens (never the secret).
func (s *Store) ListAPITokens(userID string) ([]APIToken, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, name, prefix, scopes, camera_grants, created_at, last_used, expires_at
		 FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var t APIToken
		var scopes, grants string
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &scopes, &grants, &t.CreatedAt, &t.LastUsed, &t.ExpiresAt); err != nil {
			return nil, err
		}
		t.Scopes, t.CameraGrants = csvSplit(scopes), csvSplit(grants)
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAPIToken revokes a token (scoped to its owner).
func (s *Store) DeleteAPIToken(id, userID string) error {
	return affected(s.db.Exec(`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID))
}

// ResolveAPIToken returns the owning user id plus the token's scopes and camera
// grants for a valid, unexpired token, and records its use. ok is false for
// unknown or expired tokens.
func (s *Store) ResolveAPIToken(plain string) (userID string, scopes, cameraGrants []string, ok bool) {
	h := hashToken(plain)
	var uid, sc, cg string
	var exp int64
	if err := s.db.QueryRow(`SELECT user_id, scopes, camera_grants, expires_at FROM api_tokens WHERE token_hash = ?`, h).Scan(&uid, &sc, &cg, &exp); err != nil {
		return "", nil, nil, false
	}
	if exp != 0 && time.Now().UnixMilli() > exp {
		return "", nil, nil, false
	}
	_, _ = s.db.Exec(`UPDATE api_tokens SET last_used = ? WHERE token_hash = ?`, time.Now().UnixMilli(), h)
	return uid, csvSplit(sc), csvSplit(cg), true
}
