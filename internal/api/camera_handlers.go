package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/store"
)

// handleReorderCameras persists a new camera display order (admin).
func (s *Server) handleReorderCameras(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CameraIDs []string `json:"cameraIds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.st.ReorderCameras(req.CameraIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "could not reorder cameras")
		return
	}
	s.logAudit(r, "camera.reorder", "", fmt.Sprintf("%d cameras", len(req.CameraIDs)))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTestCamera probes an RTSP URL via go2rtc and returns a preview frame so
// the operator can verify a camera connection before saving it.
func (s *Server) handleTestCamera(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RTSPMainURL string `json:"rtspMainUrl"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.RTSPMainURL) == "" {
		writeError(w, http.StatusBadRequest, "rtspMainUrl required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	jpeg, err := s.sm.TestStream(ctx, req.RTSPMainURL)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.logAudit(r, "camera.test", "", "rtsp connection test")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"snapshot": base64.StdEncoding.EncodeToString(jpeg),
	})
}

func (s *Server) handleListCameras(w http.ResponseWriter, r *http.Request) {
	cams, err := s.st.ListCameras()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(cams))
	for _, c := range cams {
		if !s.allowedCamera(r, c.ID) {
			continue // viewers only see cameras in their ACL
		}
		out = append(out, cameraView(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetCamera(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.allowedCamera(r, id) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	c, err := s.st.GetCamera(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cameraView(c))
}

// cameraReq uses pointers for optional fields so PUT updates only touch what the
// client actually sent (a partial edit must not wipe unset values).
type cameraReq struct {
	Name              string   `json:"name"`
	RTSPMainURL       string   `json:"rtspMainUrl"`
	RTSPSubURL        *string  `json:"rtspSubUrl"`
	Enabled           *bool    `json:"enabled"`
	RecordMode        string   `json:"recordMode"`
	RetentionDays     *int     `json:"retentionDays"`
	StorageQuotaBytes *int64   `json:"storageQuotaBytes"`
	MotionThreshold   *float64 `json:"motionThreshold"`
	FPS               *int     `json:"fps"`
	SortOrder         *int     `json:"sortOrder"`
	PrerollMs         *int     `json:"prerollMs"`
	PostrollMs        *int     `json:"postrollMs"`
	CooldownMs        *int     `json:"cooldownMs"`
	MinDurationMs     *int     `json:"minDurationMs"`
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
		RTSPSubURL:        derefStr(req.RTSPSubURL, ""),
		Enabled:           req.Enabled == nil || *req.Enabled,
		RecordMode:        defaultStr(req.RecordMode, store.ModeContinuous),
		RetentionDays:     derefInt(req.RetentionDays, 7),
		StorageQuotaBytes: derefInt64(req.StorageQuotaBytes, 0),
		MotionThreshold:   derefFloat(req.MotionThreshold, 0.02),
		FPS:               derefInt(req.FPS, 0),
		SortOrder:         derefInt(req.SortOrder, 0),
		PrerollMs:         derefInt(req.PrerollMs, 5000),
		PostrollMs:        derefInt(req.PostrollMs, 5000),
		CooldownMs:        derefInt(req.CooldownMs, 5000),
		MinDurationMs:     derefInt(req.MinDurationMs, 0),
	}
	if err := s.st.CreateCamera(c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create camera")
		return
	}
	s.syncStreams()
	s.logAudit(r, "camera.create", c.Name, "mode="+c.RecordMode)
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
	if req.RTSPSubURL != nil {
		c.RTSPSubURL = *req.RTSPSubURL
	}
	if req.Enabled != nil {
		c.Enabled = *req.Enabled
	}
	if req.RecordMode != "" {
		c.RecordMode = req.RecordMode
	}
	if req.RetentionDays != nil {
		c.RetentionDays = *req.RetentionDays
	}
	if req.StorageQuotaBytes != nil {
		c.StorageQuotaBytes = *req.StorageQuotaBytes
	}
	if req.MotionThreshold != nil {
		c.MotionThreshold = *req.MotionThreshold
	}
	if req.FPS != nil {
		c.FPS = *req.FPS
	}
	if req.SortOrder != nil {
		c.SortOrder = *req.SortOrder
	}
	if req.PrerollMs != nil {
		c.PrerollMs = *req.PrerollMs
	}
	if req.PostrollMs != nil {
		c.PostrollMs = *req.PostrollMs
	}
	if req.CooldownMs != nil {
		c.CooldownMs = *req.CooldownMs
	}
	if req.MinDurationMs != nil {
		c.MinDurationMs = *req.MinDurationMs
	}
	if err := s.st.UpdateCamera(c); err != nil {
		writeStoreError(w, err)
		return
	}
	s.syncStreams()
	s.logAudit(r, "camera.update", c.Name, "mode="+c.RecordMode)
	writeJSON(w, http.StatusOK, cameraView(c))
}

func (s *Server) handleDeleteCamera(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	name := id
	if c, err := s.st.GetCamera(id); err == nil {
		name = c.Name
	}
	if err := s.st.DeleteCamera(id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.syncStreams()
	s.logAudit(r, "camera.delete", name, "")
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
		if _, err := s.sm.Sync(ctx, cams); err != nil {
			s.log.Error("sync streams", "err", err)
		}
		// Pick up recording-mode / enabled changes promptly.
		s.rec.Reconcile()
		s.det.Reconcile()
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
		"prerollMs":         c.PrerollMs,
		"postrollMs":        c.PostrollMs,
		"cooldownMs":        c.CooldownMs,
		"minDurationMs":     c.MinDurationMs,
	}
}

func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func derefStr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

func derefInt(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

func derefInt64(p *int64, def int64) int64 {
	if p == nil {
		return def
	}
	return *p
}

func derefFloat(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}
