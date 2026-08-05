// Package auth handles password hashing, signed session cookies (JWT), signed
// short-lived stream tokens, and request authorization (RBAC).
package auth

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const sessionCookie = "aperture_session"

// Manager issues and verifies credentials. It is safe for concurrent use.
type Manager struct {
	jwtSecret    []byte
	streamSecret []byte
	cliToken     []byte
	secure       bool // mark cookies Secure (set when served over HTTPS)

	// apiToken resolves a Bearer string to session claims for a stored API token
	// (bound to a user). Injected from main so auth stays store-agnostic.
	apiToken func(bearer string) *SessionClaims
}

// NewManager builds a Manager from the configured secrets.
func NewManager(jwtSecret, streamSecret, cliToken []byte, secureCookies bool) *Manager {
	return &Manager{jwtSecret: jwtSecret, streamSecret: streamSecret, cliToken: cliToken, secure: secureCookies}
}

// SetAPITokenResolver wires the API-token → claims lookup (store-backed).
func (m *Manager) SetAPITokenResolver(fn func(bearer string) *SessionClaims) { m.apiToken = fn }

// APIToken resolves a Bearer token to claims for a stored API token, or nil.
func (m *Manager) APIToken(bearer string) *SessionClaims {
	if m.apiToken == nil {
		return nil
	}
	return m.apiToken(bearer)
}

// CLISession returns synthetic admin claims if the bearer token matches the
// local CLI token, or nil.
func (m *Manager) CLISession(bearer string) *SessionClaims {
	if len(m.cliToken) == 0 || bearer == "" {
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(bearer), m.cliToken) != 1 {
		return nil
	}
	return &SessionClaims{UserID: "cli", Email: "cli@local", Name: "nvrctl", Role: "admin", Class: ClassCLI}
}

// SessionClaims is the payload carried in the session cookie.
// Credential classes — how a request authenticated. Set at resolution time, not
// carried in the signed cookie. session and cli carry full user authority; a
// token is limited to its scopes and camera grants and can never exceed its
// owner.
const (
	ClassSession = "session" // cookie-authenticated user (the web app)
	ClassCLI     = "cli"     // local nvrctl token — synthetic superuser
	ClassToken   = "token"   // scoped API token (an extension)
)

type SessionClaims struct {
	UserID string `json:"uid"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Stage  string `json:"stg,omitempty"` // "" = full session; "2fa" = password ok, awaiting code
	// Runtime-only (json:"-" — derived at resolution, never serialized into the JWT).
	Class        string   `json:"-"` // ClassSession | ClassCLI | ClassToken
	Scopes       []string `json:"-"` // token scopes; empty for session/cli means all scopes
	CameraGrants []string `json:"-"` // token per-camera allowlist; empty means the owner's full ACL
	jwt.RegisteredClaims
}

// IssuePending2FA returns a short-lived token proving the password step passed,
// to be exchanged for a real session once the TOTP code is verified.
func (m *Manager) IssuePending2FA(userID string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := SessionClaims{
		UserID: userID, Stage: "2fa",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.jwtSecret)
}

// ParsePending2FA verifies a pending-2FA token and returns its user id.
func (m *Manager) ParsePending2FA(token string) (string, error) {
	c, err := m.ParseSession(token)
	if err != nil {
		return "", err
	}
	if c.Stage != "2fa" {
		return "", errors.New("not a pending 2FA token")
	}
	return c.UserID, nil
}

// HashPassword returns a bcrypt hash of the plaintext password.
func HashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(h), err
}

// VerifyPassword reports whether plain matches the stored bcrypt hash.
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// IssueSession returns a signed JWT for the user valid for ttl.
func (m *Manager) IssueSession(userID, email, name, role string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := SessionClaims{
		UserID: userID, Email: email, Name: name, Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.jwtSecret)
}

// ParseSession verifies a session token and returns its claims.
func (m *Manager) ParseSession(token string) (*SessionClaims, error) {
	claims := &SessionClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return m.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// SetSessionCookie writes the session cookie on the response.
func (m *Manager) SetSessionCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(ttl),
		MaxAge:   int(ttl.Seconds()),
	})
}

// ClearSessionCookie expires the session cookie.
func (m *Manager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/",
		HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteLaxMode,
		MaxAge: -1,
	})
}

// SessionFromRequest extracts and verifies the session cookie, if present.
func (m *Manager) SessionFromRequest(r *http.Request) (*SessionClaims, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil, err
	}
	claims, err := m.ParseSession(c.Value)
	if err != nil {
		return nil, err
	}
	if claims.Stage != "" {
		return nil, errors.New("incomplete session")
	}
	claims.Class = ClassSession
	return claims, nil
}

// streamClaims is the payload of a short-lived stream-access token.
type streamClaims struct {
	CameraID string `json:"cam"`
	jwt.RegisteredClaims
}

// StreamToken mints a signed, short-lived token granting access to a camera's
// live/recorded streams, so <video> elements and Home Assistant can fetch media
// without a session cookie or camera credentials.
func (m *Manager) StreamToken(cameraID string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := streamClaims{
		CameraID: cameraID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.streamSecret)
}

// VerifyStreamToken validates a stream token and returns the camera id.
func (m *Manager) VerifyStreamToken(token string) (string, error) {
	claims := &streamClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return m.streamSecret, nil
	})
	if err != nil {
		return "", err
	}
	return claims.CameraID, nil
}
