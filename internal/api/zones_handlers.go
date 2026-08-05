package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// motionMaskReq is the paintable ignore-region: a coarse editor grid whose
// listed cells are excluded from motion detection.
type motionMaskReq struct {
	Cols   int   `json:"cols"`
	Rows   int   `json:"rows"`
	Ignore []int `json:"ignore"`
}

func (s *Server) handleGetZones(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := s.st.GetCamera(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	raw := c.MotionZonesJSON
	if raw == "" {
		raw = `{"cols":0,"rows":0,"ignore":[]}`
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}

func (s *Server) handleSetZones(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req motionMaskReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Cols < 0 || req.Cols > 64 || req.Rows < 0 || req.Rows > 36 {
		writeError(w, http.StatusBadRequest, "invalid grid size")
		return
	}
	// Keep only valid, in-range cell indices.
	max := req.Cols * req.Rows
	clean := make([]int, 0, len(req.Ignore))
	seen := make(map[int]bool, len(req.Ignore))
	for _, i := range req.Ignore {
		if i >= 0 && i < max && !seen[i] {
			seen[i] = true
			clean = append(clean, i)
		}
	}
	req.Ignore = clean

	c, err := s.st.GetCamera(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	b, _ := json.Marshal(req)
	c.MotionZonesJSON = string(b)
	if err := s.st.UpdateCamera(c); err != nil {
		writeStoreError(w, err)
		return
	}
	s.syncStreams() // restarts the detector with the new mask
	s.logAudit(r, "camera.zones", c.Name, "")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}
