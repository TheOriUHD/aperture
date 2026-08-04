package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/TheOriUHD/aperture/internal/format"
	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/version"
)

// handleReindex rescans the recordings directory and indexes any orphaned
// segment files the database lost track of (recovering footage and making it
// manageable again). Synchronous — runs in one batch transaction.
func (s *Server) handleReindex(w http.ResponseWriter, r *http.Request) {
	added, err := s.rec.Reindex()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reindex failed: "+err.Error())
		return
	}
	s.logAudit(r, "system.reindex", "", fmt.Sprintf("%d segments recovered", added))
	writeJSON(w, http.StatusOK, map[string]any{"recovered": added})
}

// handleBackup streams a configuration backup (settings, cameras, users, rules,
// schedules, ACLs — not footage) as a downloadable JSON file.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	b, err := s.st.ExportConfig(version.Version, time.Now().Unix())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not export configuration")
		return
	}
	s.logAudit(r, "system.backup", "", fmt.Sprintf("%d tables", len(b.Tables)))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=aperture-backup-%s.json", time.Now().UTC().Format("20060102-150405")))
	_ = json.NewEncoder(w).Encode(b)
}

// handleRestore replaces all configuration from an uploaded backup. This is
// destructive and runs in a single transaction; existing sessions may break if
// the acting admin's account changes, so the UI warns and reloads.
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid upload")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	file, _, err := r.FormFile("backup")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing backup file")
		return
	}
	defer file.Close()

	var b store.ConfigBackup
	if err := json.NewDecoder(file).Decode(&b); err != nil {
		writeError(w, http.StatusBadRequest, "not a valid Aperture backup file")
		return
	}
	if len(b.Tables) == 0 {
		writeError(w, http.StatusBadRequest, "backup contains no configuration")
		return
	}
	if err := s.st.ImportConfig(&b); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.logAudit(r, "system.restore", "", fmt.Sprintf("schema v%d, %d tables", b.SchemaVersion, len(b.Tables)))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tables": len(b.Tables)})
}

const (
	siteNameKey     = "site_name"
	defaultSiteName = "Home NVR"
	overviewTempKey = "overview_temp_sensor" // HA entity id for the overview temp ("" = geo weather)
	spotlightsKey   = "spotlights_enabled"   // show the dashboard Spotlights card ("0" = hidden)
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

func (s *Server) overviewTemp() string {
	v, _ := s.st.GetSetting(overviewTempKey)
	return v
}

// spotlightsEnabled defaults to true (shown) unless explicitly disabled.
func (s *Server) spotlightsEnabled() bool {
	v, _ := s.st.GetSetting(spotlightsKey)
	return v != "0" && v != "false"
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"siteName":           s.siteName(),
		"overviewTempSensor": s.overviewTemp(),
		"spotlightsEnabled":  s.spotlightsEnabled(),
		"recordQuality":      s.recordQuality(),
	})
}

// recordQuality is the global recording-stream preference: "high" (main stream,
// default) or "low" (sub stream where a camera has one).
func (s *Server) recordQuality() string {
	if v, _ := s.st.GetSetting("record_quality"); v == "low" {
		return "low"
	}
	return "high"
}

func (s *Server) handleSetSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SiteName           *string `json:"siteName"`           // optional; "" = default
		OverviewTempSensor *string `json:"overviewTempSensor"` // optional; "" = geo weather
		SpotlightsEnabled  *bool   `json:"spotlightsEnabled"`  // optional; show the Spotlights card
		RecordQuality      *string `json:"recordQuality"`      // optional; high | low
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RecordQuality != nil {
		q := "high"
		if strings.TrimSpace(*req.RecordQuality) == "low" {
			q = "low"
		}
		if q != s.recordQuality() {
			_ = s.st.SetSetting("record_quality", q)
			s.rec.RestartAll() // re-pull each camera on the newly-preferred stream
			s.logAudit(r, "settings.update", "record quality", q)
		}
	}
	if req.SiteName != nil {
		name := strings.TrimSpace(*req.SiteName)
		if name == "" {
			name = defaultSiteName
		}
		if err := s.st.SetSetting(siteNameKey, name); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		s.logAudit(r, "settings.update", "site name", name)
	}
	if req.OverviewTempSensor != nil {
		_ = s.st.SetSetting(overviewTempKey, strings.TrimSpace(*req.OverviewTempSensor))
	}
	if req.SpotlightsEnabled != nil {
		v := "1"
		if !*req.SpotlightsEnabled {
			v = "0"
		}
		_ = s.st.SetSetting(spotlightsKey, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"siteName":           s.siteName(),
		"overviewTempSensor": s.overviewTemp(),
		"spotlightsEnabled":  s.spotlightsEnabled(),
		"recordQuality":      s.recordQuality(),
	})
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
		s.removeSegmentFile(seg)
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
	cam := req.CameraID
	if cam == "" {
		cam = "all cameras"
	}
	s.logAudit(r, "storage.purge", cam, fmt.Sprintf("%d clips, %s freed", delSeg, format.Bytes(freed)))
	writeJSON(w, http.StatusOK, map[string]any{
		"deletedSegments": delSeg,
		"deletedEvents":   delEv,
		"freedBytes":      freed,
	})
}
