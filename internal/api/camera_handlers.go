package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/store"
)

func (s *Server) handleListCameras(w http.ResponseWriter, r *http.Request) {
	cams, err := s.st.ListCameras()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(cams))
	for _, c := range cams {
		out = append(out, cameraView(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetCamera(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.GetCamera(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cameraView(c))
}

type cameraReq struct {
	Name              string  `json:"name"`
	RTSPMainURL       string  `json:"rtspMainUrl"`
	RTSPSubURL        string  `json:"rtspSubUrl"`
	Enabled           *bool   `json:"enabled"`
	RecordMode        string  `json:"recordMode"`
	RetentionDays     int     `json:"retentionDays"`
	StorageQuotaBytes int64   `json:"storageQuotaBytes"`
	MotionThreshold   float64 `json:"motionThreshold"`
	FPS               int     `json:"fps"`
	SortOrder         int     `json:"sortOrder"`
}

func (s *Server) handleCreateCamera(w http.ResponseWriter, r *http.Request) {
	var req cameraReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.RTSPMainURL) == "" {
		writeError(w, http.StatusBadRequest, "name and rtspMainUrl are required")
		return
	}
	c := &store.Camera{
		Name:              req.Name,
		RTSPMainURL:       req.RTSPMainURL,
		RTSPSubURL:        req.RTSPSubURL,
		Enabled:           req.Enabled == nil || *req.Enabled,
		RecordMode:        defaultStr(req.RecordMode, store.ModeContinuous),
		RetentionDays:     req.RetentionDays,
		StorageQuotaBytes: req.StorageQuotaBytes,
		MotionThreshold:   req.MotionThreshold,
		FPS:               req.FPS,
		SortOrder:         req.SortOrder,
	}
	if err := s.st.CreateCamera(c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create camera")
		return
	}
	s.syncStreams()
	writeJSON(w, http.StatusCreated, cameraView(c))
}

func (s *Server) handleUpdateCamera(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.GetCamera(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var req cameraReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != "" {
		c.Name = req.Name
	}
	if req.RTSPMainURL != "" {
		c.RTSPMainURL = req.RTSPMainURL
	}
	c.RTSPSubURL = req.RTSPSubURL
	if req.Enabled != nil {
		c.Enabled = *req.Enabled
	}
	if req.RecordMode != "" {
		c.RecordMode = req.RecordMode
	}
	c.RetentionDays = req.RetentionDays
	c.StorageQuotaBytes = req.StorageQuotaBytes
	c.MotionThreshold = req.MotionThreshold
	c.FPS = req.FPS
	c.SortOrder = req.SortOrder
	if err := s.st.UpdateCamera(c); err != nil {
		writeStoreError(w, err)
		return
	}
	s.syncStreams()
	writeJSON(w, http.StatusOK, cameraView(c))
}

func (s *Server) handleDeleteCamera(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteCamera(chi.URLParam(r, "id")); err != nil {
		writeStoreError(w, err)
		return
	}
	s.syncStreams()
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// syncStreams reconciles go2rtc with the current cameras in the background.
func (s *Server) syncStreams() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cams, err := s.st.ListCameras()
		if err != nil {
			s.log.Error("sync streams: list cameras", "err", err)
			return
		}
		if err := s.sm.Sync(ctx, cams); err != nil {
			s.log.Error("sync streams", "err", err)
		}
	}()
}

func cameraView(c *store.Camera) map[string]any {
	return map[string]any{
		"id":                c.ID,
		"name":              c.Name,
		"rtspMainUrl":       c.RTSPMainURL,
		"rtspSubUrl":        c.RTSPSubURL,
		"enabled":           c.Enabled,
		"recordMode":        c.RecordMode,
		"retentionDays":     c.RetentionDays,
		"storageQuotaBytes": c.StorageQuotaBytes,
		"motionThreshold":   c.MotionThreshold,
		"fps":               c.FPS,
		"sortOrder":         c.SortOrder,
	}
}

func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
