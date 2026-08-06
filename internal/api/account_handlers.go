package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/TheOriUHD/aperture/internal/auth"
)

const pending2FATTL = 5 * time.Minute

func randFrom(alpha string, n int) string {
	b := make([]byte, n)
	for i := range b {
		x, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alpha))))
		b[i] = alpha[x.Int64()]
	}
	return string(b)
}

// generatePassword returns a strong, readable temporary password.
func generatePassword() string {
	const alpha = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	s := randFrom(alpha, 16)
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

func hashCode(c string) string {
	h := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(c))))
	return hex.EncodeToString(h[:])
}

func genRecoveryCodes(n int) (plain []string, hashedJSON string) {
	const alpha = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	hashes := make([]string, 0, n)
	for i := 0; i < n; i++ {
		code := randFrom(alpha, 4) + "-" + randFrom(alpha, 4)
		plain = append(plain, code)
		hashes = append(hashes, hashCode(code))
	}
	j, _ := json.Marshal(hashes)
	return plain, string(j)
}

// consumeRecovery removes a matching recovery code; returns the new list + true.
func consumeRecovery(recoveryJSON, code string) (string, bool) {
	var hashes []string
	_ = json.Unmarshal([]byte(recoveryJSON), &hashes)
	want := hashCode(code)
	out := make([]string, 0, len(hashes))
	found := false
	for _, h := range hashes {
		if h == want && !found {
			found = true
			continue
		}
		out = append(out, h)
	}
	if !found {
		return recoveryJSON, false
	}
	j, _ := json.Marshal(out)
	return string(j), true
}

// handleLogin2FA exchanges a pending-2FA token + code for a real session.
func (s *Server) handleLogin2FA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Code  string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	uid, err := s.am.ParsePending2FA(req.Token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "your sign-in expired — please start again")
		return
	}
	u, err := s.st.GetUserByID(uid)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	code := strings.TrimSpace(req.Code)
	ok := u.TOTPEnabled && totp.Validate(code, u.TOTPSecret)
	if !ok {
		if rem, used := consumeRecovery(u.TOTPRecovery, code); used {
			_ = s.st.SetUserTOTP(u.ID, u.TOTPSecret, true, rem)
			ok = true
		}
	}
	if !ok {
		s.logAuditAs(r, u.Email, "auth.login_failed", u.Email, "invalid 2fa code")
		writeError(w, http.StatusUnauthorized, "incorrect code")
		return
	}
	s.startSession(w, u)
	s.logAuditAs(r, u.Email, "auth.login", u.Email, "2fa")
	writeJSON(w, http.StatusOK, s.userPayload(u))
}

// handleChangePassword sets a new password. The current password is required
// unless this is the forced first-login change.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	u, err := s.st.GetUserByID(c.UserID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !u.MustChangePassword {
		if u.PasswordHash == "" || !auth.VerifyPassword(u.PasswordHash, req.CurrentPassword) {
			writeError(w, http.StatusUnauthorized, "current password is incorrect")
			return
		}
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = s.st.SetUserPassword(u.ID, hash)
	_ = s.st.SetMustChangePassword(u.ID, false)
	s.logAudit(r, "auth.password_change", u.Email, "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handle2FASetup generates a fresh (not-yet-enabled) TOTP secret.
func (s *Server) handle2FASetup(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	u, err := s.st.GetUserByID(c.UserID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Aperture NVR", AccountName: u.Email})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = s.st.SetUserTOTP(u.ID, key.Secret(), false, "")
	writeJSON(w, http.StatusOK, map[string]any{"otpauthUrl": key.URL(), "secret": key.Secret()})
}

// handle2FAEnable verifies the first code, enables 2FA, and returns recovery codes.
func (s *Server) handle2FAEnable(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	u, err := s.st.GetUserByID(c.UserID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if u.TOTPSecret == "" {
		writeError(w, http.StatusBadRequest, "start setup first")
		return
	}
	if !totp.Validate(strings.TrimSpace(req.Code), u.TOTPSecret) {
		writeError(w, http.StatusBadRequest, "incorrect code — check your authenticator")
		return
	}
	plain, hashedJSON := genRecoveryCodes(10)
	_ = s.st.SetUserTOTP(u.ID, u.TOTPSecret, true, hashedJSON)
	s.logAudit(r, "auth.2fa_enabled", u.Email, "")
	writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": plain})
}

// handle2FADisable turns off 2FA after verifying a code or the password.
func (s *Server) handle2FADisable(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	var req struct {
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	u, err := s.st.GetUserByID(c.UserID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	ok := u.TOTPEnabled && totp.Validate(strings.TrimSpace(req.Code), u.TOTPSecret)
	if !ok && u.PasswordHash != "" && auth.VerifyPassword(u.PasswordHash, req.Password) {
		ok = true
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "verification failed")
		return
	}
	_ = s.st.SetUserTOTP(u.ID, "", false, "")
	s.logAudit(r, "auth.2fa_disabled", u.Email, "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
