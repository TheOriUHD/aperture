package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/ai"
	"github.com/TheOriUHD/aperture/internal/detect"
)

// handleGetDetection returns the current object-detection config.
func (s *Server) handleGetDetection(w http.ResponseWriter, r *http.Request) {
	v := s.detect.LoadConfig()
	setETag(w, v)
	writeJSON(w, http.StatusOK, v)
}

// handleSetDetection validates and persists the detection config.
func (s *Server) handleSetDetection(w http.ResponseWriter, r *http.Request) {
	if !s.precondition(w, r, etagOf(s.detect.LoadConfig())) {
		return
	}
	var c detect.Config
	if err := decodeJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	if c.Mode == detect.ModeONNX {
		c.Mode = detect.ModeLocal
	}
	switch c.Mode {
	case detect.ModeOff, detect.ModeBridge, detect.ModeLocal:
	default:
		writeError(w, http.StatusBadRequest, "mode must be off, local or bridge")
		return
	}
	switch c.Sensitivity {
	case "", "low", "normal", "high":
	default:
		writeError(w, http.StatusBadRequest, "sensitivity must be low, normal or high")
		return
	}
	if c.Device != "" && c.Device != "auto" && c.Device != "cpu" && c.Device != "cuda" {
		writeError(w, http.StatusBadRequest, "device must be auto, cpu or cuda")
		return
	}
	if c.Model != "" && c.Model != "auto" && !s.modelExists(c.Model) {
		writeError(w, http.StatusBadRequest, "unknown model")
		return
	}
	switch c.PackageMode {
	case "", "auto", "live", "separate":
	default:
		writeError(w, http.StatusBadRequest, "packageMode must be auto, live or separate")
		return
	}
	seen := map[string]bool{}
	pk := c.Packages[:0]
	for _, id := range c.Packages {
		if seen[id] {
			continue
		}
		if _, err := s.st.GetCamera(id); err != nil {
			writeError(w, http.StatusBadRequest, "packages: unknown camera")
			return
		}
		seen[id] = true
		pk = append(pk, id)
	}
	c.Packages = pk
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
	if c.Burst > detect.MaxBurst {
		c.Burst = detect.MaxBurst
	}
	if err := s.detect.SaveConfig(c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save detection config")
		return
	}
	s.logAudit(r, "detection.config", c.Mode, "object detection configured")
	s.detect.ApplyConfig()
	s.emitConfig("detection")
	writeJSON(w, http.StatusOK, c)
}

// handleGetAI reports the state of Aperture's own object recognition: devices,
// models, what a setup would download, download progress and the detector.
func (s *Server) handleGetAI(w http.ResponseWriter, r *http.Request) {
	m := s.detect.AI()
	if m == nil {
		writeError(w, http.StatusNotFound, "object recognition is not available in this build")
		return
	}
	cfg := s.detect.LoadConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      m.Status(),
		"mode":        cfg.Mode,
		"enabled":     cfg.Mode == detect.ModeLocal,
		"labels":      cfg.Labels,
		"sensitivity": cfg.Sensitivity,
		"packages":    s.detect.PackageWatches(),
	})
}

// handleEnableAI is the one-button setup: turn local recognition on and
// download whatever the hardware needs. An optional body picks the device and
// model; both default to automatic.
func (s *Server) handleEnableAI(w http.ResponseWriter, r *http.Request) {
	m := s.detect.AI()
	if m == nil {
		writeError(w, http.StatusNotFound, "object recognition is not available in this build")
		return
	}
	var req struct {
		Device string `json:"device"`
		Model  string `json:"model"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, decodeErrMsg(err))
			return
		}
	}
	if req.Device != "" && req.Device != "auto" && req.Device != "cpu" && req.Device != "cuda" {
		writeError(w, http.StatusBadRequest, "device must be auto, cpu or cuda")
		return
	}
	if req.Model != "" && req.Model != "auto" && ai.ModelByID(req.Model) == nil {
		writeError(w, http.StatusBadRequest, "unknown model")
		return
	}
	if err := s.detect.EnableAndDownload(req.Device, req.Model); err != nil {
		writeError(w, http.StatusInternalServerError, "could not enable object recognition")
		return
	}
	s.logAudit(r, "detection.enable", "local", "object recognition enabled")
	s.emitConfig("detection")
	writeJSON(w, http.StatusOK, map[string]any{"status": m.Status()})
}

// handlePlanAI says what a device/model choice would download, so the button
// can show the size before anything is fetched.
func (s *Server) handlePlanAI(w http.ResponseWriter, r *http.Request) {
	m := s.detect.AI()
	if m == nil {
		writeError(w, http.StatusNotFound, "object recognition is not available in this build")
		return
	}
	q := r.URL.Query()
	cfg := s.detect.LoadConfig()
	d, mod, bytes := m.Plan(ai.Settings{Device: q.Get("device"), Model: q.Get("model"),
		Packages: len(cfg.Packages) > 0, PackageMode: cfg.PackageMode})
	writeJSON(w, http.StatusOK, map[string]any{"device": d, "model": mod.ID, "modelName": mod.Name, "download": bytes})
}

// handleGPUSetup checks what this machine needs before Aperture can use an
// NVIDIA GPU, step by step with the commands to run (it installs nothing).
func (s *Server) handleGPUSetup(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ai.CheckGPUSetup())
}

// handleCancelAI stops a running download.
func (s *Server) handleCancelAI(w http.ResponseWriter, r *http.Request) {
	if m := s.detect.AI(); m != nil {
		m.Cancel()
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDisableAI turns object recognition off, keeping the downloads.
func (s *Server) handleDisableAI(w http.ResponseWriter, r *http.Request) {
	c := s.detect.LoadConfig()
	c.Mode = detect.ModeOff
	if err := s.detect.SaveConfig(c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save detection config")
		return
	}
	if m := s.detect.AI(); m != nil {
		m.Cancel()
	}
	s.detect.ApplyConfig()
	s.logAudit(r, "detection.disable", "off", "object recognition turned off")
	s.emitConfig("detection")
	w.WriteHeader(http.StatusNoContent)
}

// handleRemoveAIFiles deletes downloaded runtimes and models (recognition off).
func (s *Server) handleRemoveAIFiles(w http.ResponseWriter, r *http.Request) {
	m := s.detect.AI()
	if m == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := m.RemoveFiles(); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.logAudit(r, "detection.files", "removed", "object recognition downloads removed")
	w.WriteHeader(http.StatusNoContent)
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

// modelExists reports whether a model id is known (catalog or added).
func (s *Server) modelExists(id string) bool {
	if m := s.detect.AI(); m != nil {
		return m.HasModel(id)
	}
	return ai.ModelByID(id) != nil
}

// handleAddAIModel takes a detection model the operator uploads (multipart:
// "file", optional "name", "arch" and "classes"). The file is streamed to disk
// while its SHA-256 is taken; Aperture then loads it once to find its format
// and classes before keeping it.
func (s *Server) handleAddAIModel(w http.ResponseWriter, r *http.Request) {
	m := s.detect.AI()
	if m == nil {
		writeError(w, http.StatusNotFound, "object recognition is not available in this build")
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "send the model as multipart/form-data")
		return
	}
	up := ai.CustomUpload{}
	field := func(p io.Reader) string {
		b, _ := io.ReadAll(io.LimitReader(p, 64<<10))
		return string(b)
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if up.Path != "" {
				_ = os.Remove(up.Path)
			}
			writeError(w, http.StatusBadRequest, "the upload broke off")
			return
		}
		switch part.FormName() {
		case "name":
			up.Name = field(part)
		case "arch":
			up.Arch = strings.TrimSpace(field(part))
		case "classes":
			up.Classes = field(part)
		case "file":
			if up.Path != "" {
				continue
			}
			path, err := m.UploadPath()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "could not store the model")
				return
			}
			f, err := os.Create(path)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "could not store the model")
				return
			}
			h := sha256.New()
			n, err := io.Copy(io.MultiWriter(f, h), part)
			cerr := f.Close()
			up.Path, up.Size, up.SHA256 = path, n, hex.EncodeToString(h.Sum(nil))
			if err != nil || cerr != nil {
				_ = os.Remove(path)
				writeError(w, http.StatusRequestEntityTooLarge, "the upload broke off or is larger than 2 GB")
				return
			}
			if up.Name == "" {
				up.Name = strings.TrimSuffix(filepath.Base(part.FileName()), filepath.Ext(part.FileName()))
			}
		}
		part.Close()
	}
	if up.Path == "" || up.Size == 0 {
		if up.Path != "" {
			_ = os.Remove(up.Path)
		}
		writeError(w, http.StatusBadRequest, "no model file in the upload")
		return
	}
	switch up.Arch {
	case "", "yolox", "detr":
	default:
		_ = os.Remove(up.Path)
		writeError(w, http.StatusBadRequest, "arch must be yolox or detr")
		return
	}
	v, err := m.AddCustomModel(r.Context(), up)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.logAudit(r, "detection.model.add", v.ID, "detection model added: "+v.Name)
	s.emitConfig("detection")
	writeJSON(w, http.StatusOK, v)
}

// handleDeleteAIModel removes a model the operator added.
func (s *Server) handleDeleteAIModel(w http.ResponseWriter, r *http.Request) {
	m := s.detect.AI()
	if m == nil {
		writeError(w, http.StatusNotFound, "object recognition is not available in this build")
		return
	}
	id := chi.URLParam(r, "id")
	if err := m.RemoveCustomModel(id); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "no such model")
			return
		}
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.logAudit(r, "detection.model.remove", id, "detection model removed")
	s.emitConfig("detection")
	w.WriteHeader(http.StatusNoContent)
}
