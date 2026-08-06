package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/store"
)

func (s *Server) userView(u *store.User) map[string]any {
	cams, _ := s.st.GetUserCameraIDs(u.ID)
	if cams == nil {
		cams = []string{}
	}
	return map[string]any{
		"id":        u.ID,
		"email":     u.Email,
		"name":      u.Name,
		"role":      u.Role,
		"disabled":  u.Disabled,
		"cameraIds": cams,
	}
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, s.userView(u))
	}
	writeJSON(w, http.StatusOK, out)
}

type userReq struct {
	Email     string   `json:"email"`
	Name      string   `json:"name"`
	Role      string   `json:"role"`
	Password  string   `json:"password"`
	Disabled  *bool    `json:"disabled"`
	CameraIDs []string `json:"cameraIds"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req userReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	// Generate a strong temporary password unless one was supplied; the user is
	// forced to set their own on first login.
	tempPassword := strings.TrimSpace(req.Password)
	generated := false
	if tempPassword == "" {
		tempPassword = generatePassword()
		generated = true
	} else if len(tempPassword) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	role := req.Role
	if role != store.RoleAdmin {
		role = store.RoleViewer
	}
	hash, err := auth.HashPassword(tempPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	u := &store.User{Email: req.Email, Name: req.Name, PasswordHash: hash, Role: role, MustChangePassword: true}
	if err := s.st.CreateUser(u); err != nil {
		writeError(w, http.StatusConflict, "could not create user (email may already exist)")
		return
	}
	if role == store.RoleViewer {
		_ = s.st.SetUserCameras(u.ID, req.CameraIDs)
	}
	s.logAudit(r, "user.create", u.Email, "role="+u.Role)
	resp := s.userView(u)
	resp["tempPassword"] = tempPassword
	resp["generated"] = generated
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u, err := s.st.GetUserByID(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var req userReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != "" {
		u.Name = req.Name
	}
	if req.Role == store.RoleAdmin || req.Role == store.RoleViewer {
		u.Role = req.Role
	}
	disabled := u.Disabled
	if req.Disabled != nil {
		disabled = *req.Disabled
	}
	if err := s.st.UpdateUser(u.ID, u.Name, u.Role, disabled); err != nil {
		writeStoreError(w, err)
		return
	}
	if req.Password != "" {
		if len(req.Password) < 8 {
			writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		hash, _ := auth.HashPassword(req.Password)
		_ = s.st.SetUserPassword(u.ID, hash)
	}
	if req.CameraIDs != nil {
		_ = s.st.SetUserCameras(u.ID, req.CameraIDs)
	}
	u, _ = s.st.GetUserByID(id)
	s.logAudit(r, "user.update", u.Email, "role="+u.Role)
	writeJSON(w, http.StatusOK, s.userView(u))
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Don't allow deleting yourself (avoid locking out the last admin).
	if c := auth.FromContext(r.Context()); c != nil && c.UserID == id {
		writeError(w, http.StatusBadRequest, "you cannot delete your own account")
		return
	}
	email := id
	if u, err := s.st.GetUserByID(id); err == nil {
		email = u.Email
	}
	if err := s.st.DeleteUser(id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.logAudit(r, "user.delete", email, "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// allowedCameraSet returns (isAdmin, allowedSet). For admins isAdmin is true and
// the set is nil (everything allowed); for viewers the set is their ACL.
func (s *Server) allowedCameraSet(r *http.Request) (bool, map[string]bool) {
	c := auth.FromContext(r.Context())
	if c == nil {
		return false, map[string]bool{}
	}
	isAdmin := c.Role == store.RoleAdmin
	var set map[string]bool
	if !isAdmin {
		ids, _ := s.st.GetUserCameraIDs(c.UserID)
		set = make(map[string]bool, len(ids))
		for _, id := range ids {
			set[id] = true
		}
	}
	// A scoped token can never exceed its owner, and is further narrowed to its
	// camera grants (empty grants = the owner's full access). Composing with the
	// owner set guarantees the intersection, never a superset.
	if c.Class == auth.ClassToken && len(c.CameraGrants) > 0 {
		grant := make(map[string]bool, len(c.CameraGrants))
		for _, id := range c.CameraGrants {
			if isAdmin || set[id] {
				grant[id] = true
			}
		}
		return false, grant
	}
	return isAdmin, set
}

// allowedCamera reports whether the current user may access a camera (admins:
// all; viewers: their ACL).
func (s *Server) allowedCamera(r *http.Request, cameraID string) bool {
	isAdmin, set := s.allowedCameraSet(r)
	return isAdmin || set[cameraID]
}
