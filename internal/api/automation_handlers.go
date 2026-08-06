package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/store"
)

func (s *Server) handleListAutomations(w http.ResponseWriter, r *http.Request) {
	autos, err := s.st.ListAutomations()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, len(autos))
	for i := range autos {
		out[i] = withETag(autos[i]) // per-item etag for If-Match round-trips
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateAutomation(w http.ResponseWriter, r *http.Request) {
	var a store.Automation
	if err := decodeJSON(r, &a); err != nil || strings.TrimSpace(a.Name) == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	if err := s.st.CreateAutomation(&a); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create automation")
		return
	}
	s.logAudit(r, "automation.create", a.Name, "")
	s.emitAutomation()
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleUpdateAutomation(w http.ResponseWriter, r *http.Request) {
	cur, err := s.st.GetAutomation(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.precondition(w, r, etagOf(cur)) {
		return
	}
	var a store.Automation
	if err := decodeJSON(r, &a); err != nil || strings.TrimSpace(a.Name) == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	a.ID = chi.URLParam(r, "id")
	if err := s.st.UpdateAutomation(&a); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.logAudit(r, "automation.update", a.Name, "")
	s.emitAutomation()
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleDeleteAutomation(w http.ResponseWriter, r *http.Request) {
	cur, err := s.st.GetAutomation(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.precondition(w, r, etagOf(cur)) {
		return
	}
	if err := s.st.DeleteAutomation(chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete")
		return
	}
	s.emitAutomation()
	w.WriteHeader(http.StatusNoContent)
}

// handleTestAutomation runs an automation's actions against a synthetic event so
// the operator can verify the wiring from the builder.
func (s *Server) handleTestAutomation(w http.ResponseWriter, r *http.Request) {
	a, err := s.st.GetAutomation(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	// Pick a plausible camera for the synthetic event: one named in the trigger,
	// else the first camera.
	camID := ""
	var trig struct {
		Cameras []string `json:"cameras"`
	}
	_ = json.Unmarshal(a.Trigger, &trig)
	if len(trig.Cameras) > 0 {
		camID = trig.Cameras[0]
	} else if cams, err := s.st.ListCameras(); err == nil && len(cams) > 0 {
		camID = cams[0].ID
	}
	ev := &store.Event{ID: "test", CameraID: camID, Type: "motion", StartTS: time.Now().UnixMilli()}
	s.automate.Run(a.Actions, ev)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ranActions": true})
}
