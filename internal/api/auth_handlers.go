package api

import (
	"net/http"
	"strings"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/store"
)

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.st.CountUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"needsSetup": n == 0})
}

type setupReq struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

// handleSetup creates the first administrator. It is only permitted while no
// accounts exist.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	n, err := s.st.CountUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, "already configured")
		return
	}
	var req setupReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "email and a password of at least 8 characters are required")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	u := &store.User{Email: req.Email, Name: req.Name, PasswordHash: hash, Role: store.RoleAdmin}
	if err := s.st.CreateUser(u); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}
	s.startSession(w, u)
	s.logAuditAs(r, u.Email, "user.create", u.Email, "first administrator")
	writeJSON(w, http.StatusCreated, s.userPayload(u))
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	u, err := s.st.GetUserByEmail(req.Email)
	if err != nil || u.PasswordHash == "" || !auth.VerifyPassword(u.PasswordHash, req.Password) {
		s.logAuditAs(r, req.Email, "auth.login_failed", req.Email, "invalid email or password")
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if u.Disabled {
		s.logAuditAs(r, u.Email, "auth.login_failed", u.Email, "account disabled")
		writeError(w, http.StatusForbidden, "account disabled")
		return
	}
	if u.TOTPEnabled {
		tok, err := s.am.IssuePending2FA(u.ID, pending2FATTL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"twoFactorRequired": true, "token": tok})
		return
	}
	s.startSession(w, u)
	s.logAuditAs(r, u.Email, "auth.login", u.Email, "password")
	writeJSON(w, http.StatusOK, s.userPayload(u))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.logAudit(r, "auth.logout", "", "")
	s.am.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	u, err := s.st.GetUserByID(c.UserID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.userPayload(u))
}

func (s *Server) startSession(w http.ResponseWriter, u *store.User) {
	token, err := s.am.IssueSession(u.ID, u.Email, u.Name, u.Role, sessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.am.SetSessionCookie(w, token, sessionTTL)
}

func publicUser(u *store.User) map[string]any {
	return map[string]any{
		"id":    u.ID,
		"email": u.Email,
		"name":  u.Name,
		"role":  u.Role,
	}
}
