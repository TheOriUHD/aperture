package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Sign-in should last like it does in other web apps: as long as the browser
// keeps using it. And once a browser has passed the second factor, a later
// sign-in there must not ask for the code again. These pin both, plus the
// revocation that long-lived sessions make necessary.

const sessPassword = "correct-horse-battery"

// call sends a request carrying the given cookies.
func call(s *Server, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130.0")
	for _, c := range cookies {
		if c != nil {
			r.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}

// cookieOf returns the cookie a response set, or nil.
func cookieOf(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func newSessUser(t *testing.T, s *Server, id, email, role string, withTOTP bool) (*store.User, string) {
	t.Helper()
	hash, err := auth.HashPassword(sessPassword)
	mustDo(t, err)
	u := &store.User{ID: id, Email: email, Name: id, Role: role, PasswordHash: hash}
	mustDo(t, s.st.CreateUser(u))
	secret := ""
	if withTOTP {
		key, err := totp.Generate(totp.GenerateOpts{Issuer: "Aperture NVR", AccountName: email})
		mustDo(t, err)
		secret = key.Secret()
		mustDo(t, s.st.SetUserTOTP(u.ID, secret, true, ""))
	}
	return u, secret
}

// signIn runs the browser sign-in: password, then the code when asked for it.
// It returns the session and device cookies the server set.
func signIn(t *testing.T, s *Server, email, secret string, trust bool, device *http.Cookie) (sess, dev *http.Cookie, askedForCode bool) {
	t.Helper()
	rec := call(s, "POST", "/api/v1/auth/login", `{"email":"`+email+`","password":"`+sessPassword+`"}`, device)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"twoFactorRequired":true`) {
		return cookieOf(rec, "aperture_session"), device, false
	}
	var pending string
	if i := strings.Index(rec.Body.String(), `"token":"`); i >= 0 {
		pending = rec.Body.String()[i+9:]
		pending = pending[:strings.Index(pending, `"`)]
	}
	code, err := totp.GenerateCode(secret, time.Now())
	mustDo(t, err)
	trustJSON := "false"
	if trust {
		trustJSON = "true"
	}
	rec = call(s, "POST", "/api/v1/auth/login/2fa", `{"token":"`+pending+`","code":"`+code+`","trustDevice":`+trustJSON+`}`, device)
	if rec.Code != http.StatusOK {
		t.Fatalf("2fa: %d %s", rec.Code, rec.Body.String())
	}
	if d := cookieOf(rec, "aperture_device"); d != nil {
		device = d
	}
	return cookieOf(rec, "aperture_session"), device, true
}

func TestSessionCookieIsLongLivedAndStoredOnlyAsAHash(t *testing.T) {
	s := newTestServer(t)
	newSessUser(t, s, "u1", "a@t", store.RoleAdmin, false)
	sess, _, _ := signIn(t, s, "a@t", "", true, nil)
	if sess == nil || sess.MaxAge < int((89*24*time.Hour).Seconds()) || !sess.HttpOnly {
		t.Fatalf("session cookie should be HttpOnly and last ~90 days: %+v", sess)
	}
	if auth.IsLegacySession(sess.Value) {
		t.Fatal("new sign-ins must not issue the old self-contained cookie")
	}
	if _, _, err := s.st.SessionByTokenHash(auth.HashToken(sess.Value)); err != nil {
		t.Fatalf("session should be stored by the hash of its token: %v", err)
	}
	if _, _, err := s.st.SessionByTokenHash(sess.Value); err == nil {
		t.Fatal("the raw token must not be stored")
	}
}

func TestSessionSlidesForwardWithUseUpToTheCap(t *testing.T) {
	s := newTestServer(t)
	u, _ := newSessUser(t, s, "u1", "a@t", store.RoleAdmin, false)
	now := time.Now().Unix()
	tok, hash := auth.NewToken()
	// Last used two hours ago and about to lapse; the hard cap is a day away.
	mustDo(t, s.st.CreateSession(&store.Session{UserID: u.ID, TokenHash: hash, CreatedAt: now - 7200,
		LastSeenAt: now - 7200, ExpiresAt: now + 60, MaxExpiresAt: now + 86400}))
	c := &http.Cookie{Name: "aperture_session", Value: tok}

	rec := call(s, "GET", "/api/v1/auth/me", "", c)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: %d", rec.Code)
	}
	sess, _, _ := s.st.SessionByTokenHash(hash)
	if sess.ExpiresAt != sess.MaxExpiresAt || sess.LastSeenAt < now {
		t.Fatalf("use should slide the expiry, but never past the cap: %+v", sess)
	}
	if renewed := cookieOf(rec, "aperture_session"); renewed == nil || renewed.Value != tok || renewed.MaxAge > 86400 {
		t.Fatalf("the cookie should be re-issued with the slid expiry: %+v", renewed)
	}
	// Used again within the hour: no rewrite, no Set-Cookie on every request.
	if again := call(s, "GET", "/api/v1/auth/me", "", c); cookieOf(again, "aperture_session") != nil {
		t.Error("a session used minutes ago should not be rewritten")
	}
}

func TestExpiredOrUnknownSessionIsRejectedAndCleared(t *testing.T) {
	s := newTestServer(t)
	u, _ := newSessUser(t, s, "u1", "a@t", store.RoleAdmin, false)
	now := time.Now().Unix()
	tok, hash := auth.NewToken()
	mustDo(t, s.st.CreateSession(&store.Session{UserID: u.ID, TokenHash: hash, CreatedAt: now - 100,
		LastSeenAt: now - 100, ExpiresAt: now - 1, MaxExpiresAt: now + 86400}))
	for _, c := range []*http.Cookie{
		{Name: "aperture_session", Value: tok},
		{Name: "aperture_session", Value: "not-a-session"},
	} {
		rec := call(s, "GET", "/api/v1/auth/me", "", c)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%q: got %d, want 401", c.Value, rec.Code)
		}
		if cl := cookieOf(rec, "aperture_session"); cl == nil || cl.MaxAge >= 0 {
			t.Errorf("%q: the dead cookie should be cleared: %+v", c.Value, cl)
		}
	}
	if list, _ := s.st.ListSessions(u.ID, 0); len(list) != 0 {
		t.Errorf("the expired session should be deleted: %d left", len(list))
	}
}

func TestRoleAndDisableTakeEffectImmediately(t *testing.T) {
	s := newTestServer(t)
	admin, _ := newSessUser(t, s, "a1", "admin@t", store.RoleAdmin, false)
	v, _ := newSessUser(t, s, "v1", "v@t", store.RoleViewer, false)
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s, admin))}
	vc := &http.Cookie{Name: "aperture_session", Value: sessionCookieFor(t, s, v)}

	if rec := call(s, "GET", "/api/v1/users", "", vc); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer on an admin route: %d, want 403", rec.Code)
	}
	if rec := s.reqBody("PUT", "/api/v1/users/v1", adminC, strings.NewReader(`{"role":"admin"}`)); rec.Code != http.StatusOK {
		t.Fatalf("promote: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(s, "GET", "/api/v1/users", "", vc); rec.Code != http.StatusOK {
		t.Fatalf("a promotion should apply to the existing session at once: %d", rec.Code)
	}
	if rec := s.reqBody("PUT", "/api/v1/users/v1", adminC, strings.NewReader(`{"disabled":true}`)); rec.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(s, "GET", "/api/v1/auth/me", "", vc); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a disabled account must be signed out at once: %d", rec.Code)
	}
	if list, _ := s.st.ListSessions("v1", 0); len(list) != 0 {
		t.Errorf("disabling should end every session: %d left", len(list))
	}
}

func TestTrustedDeviceSkipsTheCodeAfterLogout(t *testing.T) {
	s := newTestServer(t)
	_, secret := newSessUser(t, s, "u1", "a@t", store.RoleAdmin, true)

	sess, dev, asked := signIn(t, s, "a@t", secret, true, nil)
	if !asked || sess == nil || dev == nil {
		t.Fatalf("first sign-in: asked=%v session=%v device=%v", asked, sess, dev)
	}
	if dev.MaxAge < int((364*24*time.Hour).Seconds()) || !dev.HttpOnly {
		t.Errorf("the device cookie should be HttpOnly and last ~a year: %+v", dev)
	}
	if rec := call(s, "POST", "/api/v1/auth/logout", "", sess); rec.Code != http.StatusOK {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := call(s, "GET", "/api/v1/auth/me", "", sess); rec.Code != http.StatusUnauthorized {
		t.Fatalf("logout must end the session server-side, not just drop the cookie: %d", rec.Code)
	}
	sess2, _, asked := signIn(t, s, "a@t", secret, true, dev)
	if asked || sess2 == nil {
		t.Fatalf("signing in again on a trusted browser must not ask for the code (asked=%v)", asked)
	}
	if rec := call(s, "GET", "/api/v1/auth/me", "", sess2); rec.Code != http.StatusOK {
		t.Fatalf("the new session should work: %d", rec.Code)
	}
	// A browser without the device cookie still has to give the code.
	if _, _, asked := signIn(t, s, "a@t", secret, true, nil); !asked {
		t.Error("an unknown browser must still be asked for the code")
	}
}

func TestTrustIsOptionalAndPerAccount(t *testing.T) {
	s := newTestServer(t)
	_, secretA := newSessUser(t, s, "a", "a@t", store.RoleAdmin, true)
	_, secretB := newSessUser(t, s, "b", "b@t", store.RoleAdmin, true)

	if _, dev, _ := signIn(t, s, "a@t", secretA, false, nil); dev != nil {
		t.Fatalf("declining to trust the device must not set a device cookie: %+v", dev)
	}
	_, devA, _ := signIn(t, s, "a@t", secretA, true, nil)
	if _, _, asked := signIn(t, s, "b@t", secretB, true, devA); !asked {
		t.Error("a device trusted for one account must not skip the code for another")
	}
}

func TestDisabling2FAForgetsTrustedDevices(t *testing.T) {
	s := newTestServer(t)
	_, secret := newSessUser(t, s, "u1", "a@t", store.RoleAdmin, true)
	sess, _, _ := signIn(t, s, "a@t", secret, true, nil)
	if n, _ := s.st.CountTrustedDevices("u1", 0); n != 1 {
		t.Fatalf("trusted devices = %d, want 1", n)
	}
	// Turning the authenticator off forgets every trusted device; one set up
	// later starts with none.
	rec := call(s, "POST", "/api/v1/auth/2fa/disable", `{"password":"`+sessPassword+`"}`, sess)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable 2fa: %d %s", rec.Code, rec.Body.String())
	}
	if n, _ := s.st.CountTrustedDevices("u1", 0); n != 0 {
		t.Errorf("disabling 2FA should forget trusted devices, %d left", n)
	}
}

func TestPasswordChangeSignsOutOtherBrowsers(t *testing.T) {
	s := newTestServer(t)
	newSessUser(t, s, "u1", "a@t", store.RoleAdmin, false)
	here, _, _ := signIn(t, s, "a@t", "", true, nil)
	there, _, _ := signIn(t, s, "a@t", "", true, nil)

	rec := call(s, "POST", "/api/v1/auth/password", `{"currentPassword":"`+sessPassword+`","newPassword":"another-long-one"}`, here)
	if rec.Code != http.StatusOK {
		t.Fatalf("change password: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(s, "GET", "/api/v1/auth/me", "", here); rec.Code != http.StatusOK {
		t.Errorf("the browser that changed it stays signed in: %d", rec.Code)
	}
	if rec := call(s, "GET", "/api/v1/auth/me", "", there); rec.Code != http.StatusUnauthorized {
		t.Errorf("every other browser must be signed out: %d", rec.Code)
	}
}

func TestDeviceListAndRevocation(t *testing.T) {
	s := newTestServer(t)
	_, secret := newSessUser(t, s, "u1", "a@t", store.RoleAdmin, true)
	newSessUser(t, s, "u2", "b@t", store.RoleAdmin, false)
	here, hereDev, _ := signIn(t, s, "a@t", secret, true, nil)
	phone, phoneDev, _ := signIn(t, s, "a@t", secret, true, nil)
	other, _, _ := signIn(t, s, "b@t", "", true, nil)

	rec := call(s, "GET", "/api/v1/auth/sessions", "", here)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Count(body, `"id"`) != 2 || strings.Count(body, `"current":true`) != 1 ||
		strings.Count(body, `"trusted":true`) != 2 || !strings.Contains(body, `"trustedDevices":2`) {
		t.Fatalf("device list: %d %s", rec.Code, body)
	}
	if strings.Contains(body, here.Value) || strings.Contains(body, hereDev.Value) {
		t.Fatal("the device list must never contain a cookie token")
	}

	// Another account's session cannot be revoked from here.
	otherSess, _, _ := s.st.SessionByTokenHash(auth.HashToken(other.Value))
	if rec := call(s, "DELETE", "/api/v1/auth/sessions/"+otherSess.ID, "", here); rec.Code != http.StatusNotFound {
		t.Fatalf("revoking someone else's session: %d, want 404", rec.Code)
	}
	// Signing the phone out also forgets its trust: it needs the code again.
	phoneSess, _, _ := s.st.SessionByTokenHash(auth.HashToken(phone.Value))
	if rec := call(s, "DELETE", "/api/v1/auth/sessions/"+phoneSess.ID, "", here); rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(s, "GET", "/api/v1/auth/me", "", phone); rec.Code != http.StatusUnauthorized {
		t.Errorf("the revoked phone should be signed out: %d", rec.Code)
	}
	if _, _, asked := signIn(t, s, "a@t", secret, true, phoneDev); !asked {
		t.Error("a revoked device must not skip the code any more")
	}

	// "Sign out everywhere else" keeps only this browser, and its trust.
	if rec := call(s, "POST", "/api/v1/auth/sessions/revoke-others", "", here); rec.Code != http.StatusOK {
		t.Fatalf("revoke others: %d %s", rec.Code, rec.Body.String())
	}
	if list, _ := s.st.ListSessions("u1", 0); len(list) != 1 {
		t.Errorf("only this browser's session should remain, got %d", len(list))
	}
	if n, _ := s.st.CountTrustedDevices("u1", 0); n != 1 {
		t.Errorf("only this browser's trust should remain, got %d", n)
	}
	if rec := call(s, "GET", "/api/v1/auth/me", "", other); rec.Code != http.StatusOK {
		t.Errorf("another account must be untouched: %d", rec.Code)
	}
}

func TestOldCookieIsMovedOntoAStoredSessionOnce(t *testing.T) {
	s := newTestServer(t)
	u, _ := newSessUser(t, s, "u1", "a@t", store.RoleAdmin, true)
	old, err := s.am.IssueSession(u.ID, u.Email, u.Name, u.Role, time.Hour)
	mustDo(t, err)
	oldC := &http.Cookie{Name: "aperture_session", Value: old}

	// A page load: several requests at once, all still carrying the old cookie.
	var first *http.Cookie
	for i := 0; i < 5; i++ {
		rec := call(s, "GET", "/api/v1/auth/me", "", oldC)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d with the old cookie: %d — browsers must not be signed out by the upgrade", i, rec.Code)
		}
		nc := cookieOf(rec, "aperture_session")
		if nc == nil || auth.IsLegacySession(nc.Value) || cookieOf(rec, "aperture_device") == nil {
			t.Fatalf("request %d: should be handed a stored session and a device cookie", i)
		}
		if first == nil {
			first = nc
		} else if nc.Value != first.Value {
			t.Fatal("repeated requests with the same old cookie must map to ONE session")
		}
	}
	if list, _ := s.st.ListSessions(u.ID, 0); len(list) != 1 {
		t.Fatalf("sessions after the upgrade = %d, want 1", len(list))
	}
	if n, _ := s.st.CountTrustedDevices(u.ID, 0); n != 1 {
		t.Fatalf("an account with 2FA should get its browser trusted once, got %d", n)
	}
	if rec := call(s, "GET", "/api/v1/auth/me", "", first); rec.Code != http.StatusOK {
		t.Fatalf("the replacement cookie should work: %d", rec.Code)
	}
	// A forged or expired old cookie is not upgraded.
	expired, _ := s.am.IssueSession(u.ID, u.Email, u.Name, u.Role, -time.Minute)
	for _, v := range []string{expired, old[:len(old)-2] + "xx"} {
		if rec := call(s, "GET", "/api/v1/auth/me", "", &http.Cookie{Name: "aperture_session", Value: v}); rec.Code != http.StatusUnauthorized {
			t.Errorf("an invalid old cookie must not be upgraded: %d", rec.Code)
		}
	}
}
