package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/store"
)

const maxExportMs = 4 * 3600 * 1000 // cap a single export at 4 hours

func (s *Server) isOwnerOrAdmin(r *http.Request, ownerID string) bool {
	c := auth.FromContext(r.Context())
	return c != nil && (c.UserID == ownerID || c.Role == store.RoleAdmin)
}

func (s *Server) handleCreateExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CameraID  string   `json:"cameraId"`
		CameraIDs []string `json:"cameraIds"`
		Mode      string   `json:"mode"` // single | zip | composite
		From      int64    `json:"from"`
		To        int64    `json:"to"`
		BurnIn    bool     `json:"burnIn"`
		Audio     bool     `json:"audio"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cams := req.CameraIDs
	if len(cams) == 0 && req.CameraID != "" {
		cams = []string{req.CameraID}
	}
	if len(cams) == 0 {
		writeError(w, http.StatusBadRequest, "no camera selected")
		return
	}
	for _, cid := range cams {
		if !s.allowedCamera(r, cid) {
			writeError(w, http.StatusForbidden, "access denied")
			return
		}
	}
	if req.To <= req.From {
		writeError(w, http.StatusBadRequest, "invalid time range")
		return
	}
	if req.To-req.From > maxExportMs {
		writeError(w, http.StatusBadRequest, "range too long (max 4 hours per export)")
		return
	}

	mode := req.Mode
	if len(cams) == 1 {
		mode = "single"
	} else if mode == "composite" {
		mode = "composite"
	} else {
		mode = "zip"
	}

	firstCam, err := s.st.GetCamera(cams[0])
	if err != nil {
		writeStoreError(w, err)
		return
	}
	c := auth.FromContext(r.Context())
	job := &store.ExportJob{
		UserID:     c.UserID,
		CameraID:   firstCam.ID,
		CameraName: firstCam.Name,
		CameraIDs:  cams,
		Mode:       mode,
		FromTS:     req.From,
		ToTS:       req.To,
		BurnIn:     req.BurnIn,
		Audio:      req.Audio,
		Status:     "queued",
		CreatedAt:  time.Now().Unix(),
	}
	if err := s.st.CreateExport(job); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create export")
		return
	}
	s.exp.Enqueue(job.ID)
	s.logAudit(r, "clip.export", firstCam.Name, mode)
	writeJSON(w, http.StatusCreated, job)
}

func (s *Server) handleListExports(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	jobs, err := s.st.ListExportsByUser(c.UserID, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if jobs == nil {
		jobs = []*store.ExportJob{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleGetExport(w http.ResponseWriter, r *http.Request) {
	job, err := s.st.GetExport(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.isOwnerOrAdmin(r, job.UserID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleDownloadExport(w http.ResponseWriter, r *http.Request) {
	job, err := s.st.GetExport(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.isOwnerOrAdmin(r, job.UserID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	if job.Status != "done" || job.OutputPath == "" {
		writeError(w, http.StatusConflict, "export not ready")
		return
	}
	f, err := os.Open(job.OutputPath)
	if err != nil {
		writeError(w, http.StatusGone, "export file no longer available")
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	name := exportFilename(job)
	ct := "video/mp4"
	if strings.HasSuffix(job.OutputPath, ".zip") {
		ct = "application/zip"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	s.logAudit(r, "clip.download", job.CameraName, "")
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

func (s *Server) handleDeleteExport(w http.ResponseWriter, r *http.Request) {
	job, err := s.st.GetExport(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.isOwnerOrAdmin(r, job.UserID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	if job.OutputPath != "" {
		_ = os.Remove(job.OutputPath)
	}
	_ = os.Remove(filepath.Join(s.exp.Dir(), job.ID+".mp4"))
	_ = os.Remove(filepath.Join(s.exp.Dir(), job.ID+".zip"))
	_ = s.st.DeleteExport(job.ID)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func exportFilename(j *store.ExportJob) string {
	t := time.UnixMilli(j.FromTS).Format("2006-01-02_15-04-05")
	safe := ""
	for _, c := range j.CameraName {
		if c == ' ' {
			safe += "_"
		} else if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			safe += string(c)
		}
	}
	if safe == "" {
		safe = "clip"
	}
	ext := ".mp4"
	if strings.HasSuffix(j.OutputPath, ".zip") {
		ext = ".zip"
		if len(j.CameraIDs) > 1 {
			safe = "aperture-clips"
		}
	}
	return safe + "_" + t + ext
}
