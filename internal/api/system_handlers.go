package api

import (
	"net/http"
	"os"
	"strings"

	"github.com/TheOriUHD/aperture/internal/version"
)

const (
	siteNameKey     = "site_name"
	defaultSiteName = "Home NVR"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version": version.Version,
		"commit":  version.Commit,
	})
}

func (s *Server) siteName() string {
	if n, err := s.st.GetSetting(siteNameKey); err == nil && n != "" {
		return n
	}
	return defaultSiteName
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"siteName": s.siteName()})
}

func (s *Server) handleSetSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SiteName string `json:"siteName"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.SiteName)
	if name == "" {
		name = defaultSiteName
	}
	if err := s.st.SetSetting(siteNameKey, name); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"siteName": name})
}

// handlePurge deletes recorded footage (and events) in a time range, optionally
// scoped to one camera. Admin only.
func (s *Server) handlePurge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CameraID string `json:"cameraId"`
		From     int64  `json:"from"`
		To       int64  `json:"to"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.To <= req.From {
		writeError(w, http.StatusBadRequest, "invalid time range")
		return
	}

	segs, err := s.st.SegmentsInRange(req.CameraID, req.From, req.To)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var freed int64
	delSeg := 0
	for _, seg := range segs {
		if err := os.Remove(seg.Path); err != nil && !os.IsNotExist(err) {
			continue
		}
		if s.st.DeleteSegment(seg.ID) == nil {
			freed += seg.SizeBytes
			delSeg++
		}
	}

	evs, _ := s.st.EventsInRange(req.CameraID, req.From, req.To)
	delEv := 0
	for _, e := range evs {
		if e.ThumbPath != "" {
			_ = os.Remove(e.ThumbPath)
		}
		if s.st.DeleteEvent(e.ID) == nil {
			delEv++
		}
	}

	s.log.Info("storage purge", "camera", req.CameraID, "segments", delSeg, "events", delEv, "freedBytes", freed)
	writeJSON(w, http.StatusOK, map[string]any{
		"deletedSegments": delSeg,
		"deletedEvents":   delEv,
		"freedBytes":      freed,
	})
}
