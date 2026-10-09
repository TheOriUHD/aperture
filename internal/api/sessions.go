package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Browser sign-in lasts as long as the browser keeps using it, like other web
// apps: a session slides forward on use and ends only after sessionIdle without
// any, or sessionMax after sign-in. A browser that completed a second factor is
// remembered as a trusted device, so signing in again there needs only the
// password, never the authenticator code.
const (
	sessionIdle      = 90 * 24 * time.Hour  // signed out after this long unused
	sessionMax       = 365 * 24 * time.Hour // signed out this long after sign-in, used or not
	sessionTouchStep = time.Hour            // use is written back at most this often
	deviceTrustTTL   = 365 * 24 * time.Hour // trust lasts this long after the device's last use
	maxUALen         = 300
)

// legacyUpgradeWindow: how long one old-format cookie keeps mapping to the stored
// session it was converted into. A page load sends many requests at once, all
// carrying the old cookie until the first reply's Set-Cookie lands.
const legacyUpgradeWindow = 10 * time.Minute

type legacyUpgrade struct {
	sessionID, sessionToken string
	deviceToken             string // "" when the account has no second factor to remember
	at                      time.Time
}

type legacyUpgrades struct {
	mu sync.Mutex
	m  map[string]legacyUpgrade // HashToken(old cookie) -> its replacement
}

// resolveSession maps a session cookie to the signed-in user's CURRENT claims.
// Role, name and disabled state come from the users row on every request, so a
// demotion or a disabled account takes effect at once instead of when a cookie
// expires. It is wired into auth.Manager by NewServer.
func (s *Server) resolveSession(w http.ResponseWriter, r *http.Request, token string) *auth.SessionClaims {
	if auth.IsLegacySession(token) {
		return s.upgradeLegacySession(w, r, token)
	}
	sess, u, err := s.st.SessionByTokenHash(auth.HashToken(token))
	if err == store.ErrNotFound {
		s.am.ClearSessionCookie(w) // revoked or unknown: stop the browser resending it
		return nil
	}
	if err != nil {
		return nil // a database hiccup is not a reason to sign anyone out
	}
	now := time.Now().Unix()
	if now >= sess.ExpiresAt || now >= sess.MaxExpiresAt || u.Disabled {
		_ = s.st.DeleteSession(sess.ID)
		s.am.ClearSessionCookie(w)
		return nil
	}
	if now-sess.LastSeenAt >= int64(sessionTouchStep/time.Second) {
		exp := min(now+int64(sessionIdle/time.Second), sess.MaxExpiresAt)
		if s.st.TouchSession(sess.ID, now, exp) == nil {
			s.am.SetSessionCookie(w, token, time.Duration(exp-now)*time.Second)
		}
		// A device that keeps being used stays trusted.
		if sess.DeviceID != "" {
			s.touchTrustedDevice(w, r, sess.DeviceID, now)
		}
	}
	return &auth.SessionClaims{UserID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role, SessionID: sess.ID}
}

// startSession signs u in on this browser: a new stored session and its cookie.
// deviceID links the session to the browser's trusted device, if it has one.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u *store.User, deviceID string) error {
	now := time.Now()
	_ = s.st.PurgeExpiredSessions(now.Unix())
	token, hash := auth.NewToken()
	sess := &store.Session{
		UserID: u.ID, TokenHash: hash, DeviceID: deviceID,
		CreatedAt: now.Unix(), LastSeenAt: now.Unix(),
		ExpiresAt:    now.Add(sessionIdle).Unix(),
		MaxExpiresAt: now.Add(sessionMax).Unix(),
		IP:           clientIP(r), UA: userAgent(r),
	}
	if err := s.st.CreateSession(sess); err != nil {
		return err
	}
	s.am.SetSessionCookie(w, token, sessionIdle)
	return nil
}

// trustedDevice returns the id of this browser's trusted device for userID, or ""
// when it has none (no cookie, another account's, expired or forgotten).
func (s *Server) trustedDevice(r *http.Request, userID string) string {
	tok := auth.DeviceToken(r)
	if tok == "" {
		return ""
	}
	d, err := s.st.TrustedDeviceByTokenHash(auth.HashToken(tok))
	if err != nil || d.UserID != userID || time.Now().Unix() >= d.ExpiresAt {
		return ""
	}
	return d.ID
}

// trustDevice remembers this browser for u after a completed second factor and
// returns the device id. A browser already trusted for u keeps its device.
func (s *Server) trustDevice(w http.ResponseWriter, r *http.Request, u *store.User) string {
	now := time.Now().Unix()
	if id := s.trustedDevice(r, u.ID); id != "" {
		s.touchTrustedDevice(w, r, id, now)
		return id
	}
	token, hash := auth.NewToken()
	d := &store.TrustedDevice{
		UserID: u.ID, TokenHash: hash, CreatedAt: now, LastUsedAt: now,
		ExpiresAt: now + int64(deviceTrustTTL/time.Second),
		IP:        clientIP(r), UA: userAgent(r),
	}
	if err := s.st.CreateTrustedDevice(d); err != nil {
		return ""
	}
	s.am.SetDeviceCookie(w, token, deviceTrustTTL)
	return d.ID
}

// touchTrustedDevice slides a trusted device's expiry and, when this browser
// holds its cookie, re-issues the cookie so the browser keeps it as long too.
func (s *Server) touchTrustedDevice(w http.ResponseWriter, r *http.Request, id string, now int64) {
	if s.st.TouchTrustedDevice(id, now, now+int64(deviceTrustTTL/time.Second)) != nil {
		return
	}
	if tok := auth.DeviceToken(r); tok != "" {
		if d, err := s.st.TrustedDeviceByTokenHash(auth.HashToken(tok)); err == nil && d.ID == id {
			s.am.SetDeviceCookie(w, tok, deviceTrustTTL)
		}
	}
}

// upgradeLegacySession moves a browser still holding an old self-contained cookie
// onto a stored session, instead of signing it out at deploy. The old cookie was
// only ever issued after a complete sign-in, second factor included, so on an
// account with an authenticator the browser is also trusted as a device. Old
// cookies expire within 7 days of issue, after which this path is dead code.
func (s *Server) upgradeLegacySession(w http.ResponseWriter, r *http.Request, token string) *auth.SessionClaims {
	old, err := s.am.ParseLegacySession(token)
	if err != nil {
		s.am.ClearSessionCookie(w)
		return nil
	}
	u, err := s.st.GetUserByID(old.UserID)
	if err != nil || u.Disabled {
		s.am.ClearSessionCookie(w)
		return nil
	}
	key := auth.HashToken(token)
	s.legacy.mu.Lock()
	defer s.legacy.mu.Unlock()
	if s.legacy.m == nil {
		s.legacy.m = map[string]legacyUpgrade{}
	}
	for k, v := range s.legacy.m {
		if time.Since(v.at) > legacyUpgradeWindow {
			delete(s.legacy.m, k)
		}
	}
	up, ok := s.legacy.m[key]
	if !ok {
		deviceID, deviceToken := "", ""
		if u.TOTPEnabled {
			now := time.Now().Unix()
			var hash string
			deviceToken, hash = auth.NewToken()
			d := &store.TrustedDevice{
				UserID: u.ID, TokenHash: hash, CreatedAt: now, LastUsedAt: now,
				ExpiresAt: now + int64(deviceTrustTTL/time.Second),
				IP:        clientIP(r), UA: userAgent(r),
			}
			if s.st.CreateTrustedDevice(d) == nil {
				deviceID = d.ID
			} else {
				deviceToken = ""
			}
		}
		now := time.Now()
		sessToken, hash := auth.NewToken()
		sess := &store.Session{
			UserID: u.ID, TokenHash: hash, DeviceID: deviceID,
			CreatedAt: now.Unix(), LastSeenAt: now.Unix(),
			ExpiresAt:    now.Add(sessionIdle).Unix(),
			MaxExpiresAt: now.Add(sessionMax).Unix(),
			IP:           clientIP(r), UA: userAgent(r),
		}
		if err := s.st.CreateSession(sess); err != nil {
			return nil
		}
		up = legacyUpgrade{sessionID: sess.ID, sessionToken: sessToken, deviceToken: deviceToken, at: now}
		s.legacy.m[key] = up
		s.log.Info("auth: moved a browser onto a stored session", "user", u.Email, "trusted", deviceToken != "")
	}
	s.am.SetSessionCookie(w, up.sessionToken, sessionIdle)
	if up.deviceToken != "" {
		s.am.SetDeviceCookie(w, up.deviceToken, deviceTrustTTL)
	}
	return &auth.SessionClaims{UserID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role, SessionID: up.sessionID}
}

// endSessions signs a user out everywhere: every session, and every device that
// could skip the second factor. Used when an account is disabled.
func (s *Server) endSessions(userID string) {
	_, _ = s.st.DeleteUserSessions(userID, "")
	_, _ = s.st.DeleteUserTrustedDevices(userID, "")
}

// --- the signed-in user's own devices ---------------------------------------

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	now := time.Now().Unix()
	sessions, err := s.st.ListSessions(c.UserID, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(sessions))
	for _, x := range sessions {
		trusted := false
		if x.DeviceID != "" {
			if d, err := s.st.GetTrustedDevice(x.DeviceID); err == nil && now < d.ExpiresAt {
				trusted = true
			}
		}
		out = append(out, map[string]any{
			"id":           x.ID,
			"createdAtMs":  x.CreatedAt * 1000,
			"lastSeenAtMs": x.LastSeenAt * 1000,
			"expiresAtMs":  x.ExpiresAt * 1000,
			"ip":           x.IP,
			"userAgent":    x.UA,
			"current":      x.ID == c.SessionID,
			"trusted":      trusted,
		})
	}
	n, _ := s.st.CountTrustedDevices(c.UserID, now)
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out, "trustedDevices": n})
}

// handleRevokeSession signs one of the caller's own devices out and forgets its
// trust, so a lost or sold device needs the full sign-in, code included.
func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	sess, err := s.st.GetSession(chi.URLParam(r, "id"))
	if err != nil || sess.UserID != c.UserID {
		writeErrorCode(w, http.StatusNotFound, "not_found", "no such session")
		return
	}
	_ = s.st.DeleteSession(sess.ID)
	if sess.DeviceID != "" {
		_ = s.st.DeleteTrustedDevice(sess.DeviceID)
	}
	if sess.ID == c.SessionID {
		s.am.ClearSessionCookie(w)
		s.am.ClearDeviceCookie(w)
	}
	s.logAudit(r, "auth.session_revoked", c.Email, userAgent(r))
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handleRevokeOtherSessions signs out every other device and forgets every other
// trusted device, keeping only the browser making the request.
func (s *Server) handleRevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	keepDevice := ""
	if sess, err := s.st.GetSession(c.SessionID); err == nil {
		keepDevice = sess.DeviceID
	}
	n, err := s.st.DeleteUserSessions(c.UserID, c.SessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	d, _ := s.st.DeleteUserTrustedDevices(c.UserID, keepDevice)
	s.logAudit(r, "auth.sessions_revoked", c.Email, "others")
	writeJSON(w, http.StatusOK, map[string]any{"signedOut": n, "forgotten": d})
}

func userAgent(r *http.Request) string {
	ua := r.UserAgent()
	if len(ua) > maxUALen {
		ua = ua[:maxUALen]
	}
	return ua
}
