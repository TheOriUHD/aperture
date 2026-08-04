package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/store"
)

// handleListGroups returns the admin-defined camera groups (any authenticated
// user; the built-in "All Cameras" group is synthesised client-side).
func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.st.ListCameraGroups()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if groups == nil {
		groups = []*store.CameraGroup{}
	}
	writeJSON(w, http.StatusOK, groups)
}

type groupReq struct {
	Name      string   `json:"name"`
	CameraIDs []string `json:"cameraIds"`
	SortOrder int      `json:"sortOrder"`
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req groupReq
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "a group name is required")
		return
	}
	g := &store.CameraGroup{Name: req.Name, CameraIDs: req.CameraIDs, SortOrder: req.SortOrder}
	if err := s.st.CreateCameraGroup(g); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create group")
		return
	}
	s.logAudit(r, "group.create", g.Name, "")
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	g, err := s.st.GetCameraGroup(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var req groupReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != "" {
		g.Name = req.Name
	}
	if req.CameraIDs != nil {
		g.CameraIDs = req.CameraIDs
	}
	g.SortOrder = req.SortOrder
	if err := s.st.UpdateCameraGroup(g); err != nil {
		writeStoreError(w, err)
		return
	}
	s.logAudit(r, "group.update", g.Name, "")
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.st.DeleteCameraGroup(id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.logAudit(r, "group.delete", id, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleSetCameraGroups sets which groups a camera belongs to (the camera-side
// assignment path).
func (s *Server) handleSetCameraGroups(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.st.GetCamera(id); err != nil {
		writeStoreError(w, err)
		return
	}
	var req struct {
		GroupIDs []string `json:"groupIds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.st.SetCameraGroups(id, req.GroupIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update groups")
		return
	}
	s.logAudit(r, "camera.groups", id, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
