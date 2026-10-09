// Package auth handles password hashing, signed session cookies (JWT), signed
// short-lived stream tokens, and request authorization (RBAC).
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie = "aperture_session"
	// deviceCookie marks a browser that completed a second factor, so its next
	// password sign-in can skip the code. It outlives sessions on purpose.
	deviceCookie = "aperture_device"
)

// Manager issues and verifies credentials. It is safe for concurrent use.
type Manager struct {
	jwtSecret    []byte
	streamSecret []byte
	cliToken     []byte
	secure       bool // mark cookies Secure (set when served over HTTPS)

	// apiToken resolves a Bearer string to session claims for a stored API token
	// (bound to a user). Injected from main so auth stays store-agnostic.
	apiToken func(bearer string) *SessionClaims
	// session resolves a session cookie to the signed-in user's current claims,
	// sliding the cookie forward through w when due. Injected by the API server,
	// which owns the session store.
	session func(w http.ResponseWriter, r *http.Request, token string) *SessionClaims
}

// SetSessionResolver wires the session-cookie → claims lookup (store-backed).
func (m *Manager) SetSessionResolver(fn func(w http.ResponseWriter, r *http.Request, token string) *SessionClaims) {
	m.session = fn
}

// NewToken returns a random 256-bit cookie token and the hash that is stored for
// it. Only the hash is kept server-side, so a copy of the database cannot be
// replayed as a sign-in.
func NewToken() (token, hash string) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("auth: cannot read random bytes: " + err.Error())
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token)
}

// HashToken is the stored form of a cookie token.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// IsLegacySession reports whether a session cookie is in the old self-contained
// JWT format (three dot-separated parts) rather than a stored-session token.
func IsLegacySession(token string) bool { return strings.Count(token, ".") == 2 }

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
	ExtID        string   `json:"-"` // extension id for an extension-minted token; "" otherwise (T2 ext_kv)
	SessionID    string   `json:"-"` // the stored session behind a cookie; "" for other classes
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

// IssueSession returns a signed JWT session for the user valid for ttl: the OLD
// cookie format, replaced by stored sessions. Nothing issues it any more; it stays
// so the one-time upgrade of cookies still in browsers can be tested. Remove it
// together with ParseLegacySession once those have expired (7 days after deploy).
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

// ParseLegacySession verifies an old-format JWT session cookie, so a browser still
// carrying one can be moved onto a stored session instead of being signed out.
func (m *Manager) ParseLegacySession(token string) (*SessionClaims, error) {
	claims, err := m.ParseSession(token)
	if err != nil {
		return nil, err
	}
	if claims.Stage != "" {
		return nil, errors.New("incomplete session")
	}
	return claims, nil
}

// SetSessionCookie writes the session cookie on the response.
func (m *Manager) SetSessionCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	m.setCookie(w, sessionCookie, token, ttl)
}

// ClearSessionCookie expires the session cookie.
func (m *Manager) ClearSessionCookie(w http.ResponseWriter) { m.setCookie(w, sessionCookie, "", -1) }

// SetDeviceCookie writes the trusted-device cookie on the response.
func (m *Manager) SetDeviceCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	m.setCookie(w, deviceCookie, token, ttl)
}

// ClearDeviceCookie expires the trusted-device cookie.
func (m *Manager) ClearDeviceCookie(w http.ResponseWriter) { m.setCookie(w, deviceCookie, "", -1) }

// DeviceToken returns the trusted-device cookie's value, or "".
func DeviceToken(r *http.Request) string {
	if c, err := r.Cookie(deviceCookie); err == nil {
		return c.Value
	}
	return ""
}

// setCookie writes an HttpOnly, Lax, whole-site cookie; a negative ttl deletes it.
func (m *Manager) setCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	c := &http.Cookie{
		Name: name, Value: value, Path: "/",
		HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteLaxMode,
		MaxAge: -1,
	}
	if ttl > 0 {
		c.Expires = time.Now().Add(ttl)
		c.MaxAge = int(ttl.Seconds())
	}
	http.SetCookie(w, c)
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
