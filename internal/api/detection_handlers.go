package api

import (
	"net/http"
	"strings"

	"github.com/TheOriUHD/aperture/internal/detect"
)

// handleGetDetection returns the current object-detection config.
func (s *Server) handleGetDetection(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.detect.LoadConfig())
}

// handleSetDetection validates and persists the detection config.
func (s *Server) handleSetDetection(w http.ResponseWriter, r *http.Request) {
	var c detect.Config
	if err := decodeJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch c.Mode {
	case detect.ModeOff, detect.ModeBridge, detect.ModeONNX:
	default:
		writeError(w, http.StatusBadRequest, "mode must be off, bridge, or onnx")
		return
	}
	if c.Mode == detect.ModeBridge && strings.TrimSpace(c.BridgeURL) == "" {
		writeError(w, http.StatusBadRequest, "bridge mode requires a detector URL")
		return
	}
	if c.BridgeAPI == "" {
		c.BridgeAPI = "deepstack"
	}
	if c.MinConfidence < 0 {
		c.MinConfidence = 0
	}
	if c.MinConfidence > 1 {
		c.MinConfidence = 1
	}
	if c.Burst < 1 {
		c.Burst = 1
	}
	if c.Burst > 5 {
		c.Burst = 5
	}
	if err := s.detect.SaveConfig(c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save detection config")
		return
	}
	s.logAudit(r, "detection.config", c.Mode, "object detection configured")
	writeJSON(w, http.StatusOK, c)
}

// handleTestDetection runs the active backend on a fresh snapshot from a camera
// and returns whatever it detects (no thresholding), so the operator can verify
// the setup and tune from the Settings panel.
func (s *Server) handleTestDetection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CameraID string `json:"cameraId"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.CameraID) == "" {
		writeError(w, http.StatusBadRequest, "cameraId required")
		return
	}
	backend, dets, err := s.detect.Test(req.CameraID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "backend": backend, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "backend": backend, "detections": dets})
}
