package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
	"github.com/TheOriUHD/aperture/internal/urlcred"
)

// handleReorderCameras persists a new camera display order (admin).
func (s *Server) handleReorderCameras(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CameraIDs []string `json:"cameraIds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
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
// the operator can verify a camera connection before saving it. An existing
// camera's link arrives with its login masked; cameraId names the camera whose
// saved login fills it back in, under the same rules as a save.
func (s *Server) handleTestCamera(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RTSPMainURL string `json:"rtspMainUrl"`
		CameraID    string `json:"cameraId"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	src := strings.TrimSpace(req.RTSPMainURL)
	if src == "" {
		writeError(w, http.StatusBadRequest, "rtspMainUrl required")
		return
	}
	if urlcred.HasMask(src) {
		cam, err := s.st.GetCamera(req.CameraID)
		if req.CameraID == "" || err != nil {
			writeError(w, http.StatusBadRequest, newCameraMaskMsg)
			return
		}
		if src, err = urlcred.Merge(src, cam.RTSPMainURL, cam.RTSPSubURL); err != nil {
			writeError(w, http.StatusBadRequest, linkErrMsg("Main stream", err))
			return
		}
	}
	src = stream.NormalizeSource(src)
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	jpeg, err := s.sm.TestStream(ctx, src)
	if err != nil {
		// The link may carry a saved login filled in above; never echo it back.
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": urlcred.Scrub(err.Error())})
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
	links := seesCameraLinks(r)
	disabled := seesDisabledCameras(r)
	out := make([]map[string]any, 0, len(cams))
	for _, c := range cams {
		if !s.allowedCamera(r, c.ID) {
			continue // viewers only see cameras in their ACL
		}
		if !c.Enabled && !disabled {
			continue
		}
		v := cameraView(c, links)
		v["recordingNow"] = s.recordingNow(c)
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

// seesDisabledCameras reports whether a caller's camera lists include disabled
// cameras. An outside integration's don't: a disabled camera streams nothing and
// records nothing, so a dashboard could only draw a dead tile for it, and the web
// app hides them from its own dashboard for the same reason. The web app and nvrctl
// do get them, because that is where one is switched back on, and so do extensions,
// which can be granted a disabled camera (Tapo Control is). An integration that
// manages cameras asks with ?includeDisabled=true.
func seesDisabledCameras(r *http.Request) bool {
	c := auth.FromContext(r.Context())
	if c == nil || c.Class != auth.ClassToken || c.ExtID != "" {
		return true
	}
	return r.URL.Query().Get("includeDisabled") == "true"
}

func (s *Server) handleGetCamera(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := s.st.GetCamera(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	v := cameraView(c, seesCameraLinks(r))
	setETag(w, v)
	v["recordingNow"] = s.recordingNow(c) // after the ETag: it flips with the schedule
	writeJSON(w, http.StatusOK, v)
}

// seesCameraLinks reports whether the caller is shown camera links at all. Only
// admins (the web app, nvrctl, admin-owned tokens) are, and only with the login
// masked; a viewer has no use for a camera's address.
func seesCameraLinks(r *http.Request) bool {
	c := auth.FromContext(r.Context())
	return c != nil && c.Role == store.RoleAdmin
}

// newCameraMaskMsg answers a masked link where no saved login exists to fill it
// from, e.g. a link copied from another camera into the add form.
const newCameraMaskMsg = "This camera has no saved login yet. Replace the dots with the camera's username and password."

// linkErrMsg words a urlcred refusal for the camera editor, naming the field.
func linkErrMsg(field string, err error) string {
	return field + " link: " + err.Error() + "."
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
	MotionDetection   *bool    `json:"motionDetection"`
	AudioDetection    *bool    `json:"audioDetection"`
	AudioThreshold    *float64 `json:"audioThreshold"`
	LocationID        *string  `json:"locationId"`   // storage location for new footage ("" = default)
	DetectMethod      *string  `json:"detectMethod"` // "" = inherit; off|bridge|onnx
	TempSensor        *string  `json:"tempSensor"`
	Vendor            *string  `json:"vendor"` // HA sensor entity id ("" = weather)
	FPS               *int     `json:"fps"`
	SortOrder         *int     `json:"sortOrder"`
	PrerollMs         *int     `json:"prerollMs"`
	PostrollMs        *int     `json:"postrollMs"`
	CooldownMs        *int     `json:"cooldownMs"`
	MinDurationMs     *int     `json:"minDurationMs"`
	RecordHeight      *int     `json:"recordHeight"` // recording quality; 0 = as the camera sends it
	RecordFPS         *int     `json:"recordFps"`
	RecordKbps        *int     `json:"recordKbps"`
	HAPublish         *bool    `json:"haPublish"`
}

// validDetectMethod clamps a per-camera object-detection override to a known
// value; "" means inherit the global Settings backend.
func validDetectMethod(m string) string {
	switch m {
	case "off", "bridge", "local":
		return m
	case "onnx": // the older name for local
		return "local"
	default:
		return ""
	}
}

func (s *Server) handleCreateCamera(w http.ResponseWriter, r *http.Request) {
	var req cameraReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	if msg := recordQualityProblem(req); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.RTSPMainURL) == "" {
		writeError(w, http.StatusBadRequest, "name and rtspMainUrl are required")
		return
	}
	if urlcred.HasMask(req.RTSPMainURL) || urlcred.HasMask(derefStr(req.RTSPSubURL, "")) {
		writeError(w, http.StatusBadRequest, newCameraMaskMsg)
		return
	}
	mainURL := stream.NormalizeSource(req.RTSPMainURL)
	// A UniFi Protect camera is live view only unless asked otherwise: Protect does
	// the recording and the motion detection, and Home Assistant usually has the
	// camera already, through its own Protect integration.
	protect := stream.IsProtectSource(mainURL)
	mode, motion, haHidden := store.ModeContinuous, !protect, protect
	if protect {
		mode = store.ModeNever
	}
	if req.MotionDetection != nil {
		motion = *req.MotionDetection
	}
	if req.HAPublish != nil {
		haHidden = !*req.HAPublish
	}
	c := &store.Camera{
		Name:              req.Name,
		RTSPMainURL:       mainURL,
		RTSPSubURL:        stream.NormalizeSource(derefStr(req.RTSPSubURL, "")),
		Enabled:           req.Enabled == nil || *req.Enabled,
		RecordMode:        defaultStr(req.RecordMode, mode),
		HAHidden:          haHidden,
		RetentionDays:     derefInt(req.RetentionDays, 7),
		StorageQuotaBytes: derefInt64(req.StorageQuotaBytes, 0),
		MotionThreshold:   derefFloat(req.MotionThreshold, 0.02),
		MotionDetection:   motion,
		AudioDetection:    req.AudioDetection != nil && *req.AudioDetection,
		AudioThreshold:    derefFloat(req.AudioThreshold, 0),
		LocationID:        s.resolveLocationID(derefStr(req.LocationID, "")),
		DetectMethod:      validDetectMethod(derefStr(req.DetectMethod, "")),
		TempSensor:        derefStr(req.TempSensor, ""),
		FPS:               derefInt(req.FPS, 0),
		SortOrder:         derefInt(req.SortOrder, 0),
		PrerollMs:         derefInt(req.PrerollMs, 5000),
		PostrollMs:        derefInt(req.PostrollMs, 5000),
		CooldownMs:        derefInt(req.CooldownMs, 5000),
		MinDurationMs:     derefInt(req.MinDurationMs, 0),
		RecordHeight:      derefInt(req.RecordHeight, 0),
		RecordFPS:         derefInt(req.RecordFPS, 0),
		RecordKbps:        derefInt(req.RecordKbps, 0),
	}
	if err := s.st.CreateCamera(c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create camera")
		return
	}
	s.syncStreams()
	s.logAudit(r, "camera.create", c.Name, "mode="+c.RecordMode)
	writeJSON(w, http.StatusCreated, cameraView(c, true))
}

// recordQualityProblem words what is wrong with a request's recording quality, or
// returns "". The limits only catch nonsense; 0 means "as the camera sends it".
func recordQualityProblem(req cameraReq) string {
	bad := func(v *int, lo, hi int) bool { return v != nil && *v != 0 && (*v < lo || *v > hi) }
	switch {
	case bad(req.RecordHeight, 144, 4320):
		return "recordHeight must be 0 or between 144 and 4320"
	case bad(req.RecordFPS, 1, 120):
		return "recordFps must be 0 or between 1 and 120"
	case bad(req.RecordKbps, 100, 100000):
		return "recordKbps must be 0 or between 100 and 100000"
	}
	return ""
}

// validVendor normalises a vendor marking to one Aperture actually acts on. An
// unknown value is stored as "" rather than kept verbatim: a marking that drives
// nothing but LOOKS set is worse than no marking, because the operator would be left
// wondering why the vendor controls never appeared.
func validVendor(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case store.VendorReolink:
		return store.VendorReolink
	default:
		return ""
	}
}

func (s *Server) handleUpdateCamera(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.GetCamera(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.precondition(w, r, etagOf(cameraView(c, true))) {
		return
	}
	var req cameraReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	if msg := recordQualityProblem(req); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if req.Name != "" {
		c.Name = req.Name
	}
	// Links come back as they were shown: with the login masked. A mask means
	// "keep the saved login" and is filled in again, but only for the host it was
	// saved for (urlcred.Merge); anything else is taken as typed. A client that
	// leaves a link out keeps it untouched.
	oldMain, oldSub, haBefore := c.RTSPMainURL, c.RTSPSubURL, c.HAHidden
	if req.RTSPMainURL != "" {
		u, err := urlcred.Merge(req.RTSPMainURL, oldMain, oldSub)
		if err != nil {
			writeError(w, http.StatusBadRequest, linkErrMsg("Main stream", err))
			return
		}
		c.RTSPMainURL = stream.NormalizeSource(u)
	}
	if req.RTSPSubURL != nil {
		u, err := urlcred.Merge(*req.RTSPSubURL, oldSub, oldMain)
		if err != nil {
			writeError(w, http.StatusBadRequest, linkErrMsg("Sub stream", err))
			return
		}
		c.RTSPSubURL = stream.NormalizeSource(u)
	}
	c.RTSPSubURL = urlcred.Follow(c.RTSPSubURL, oldMain, c.RTSPMainURL)
	loginChanged := !urlcred.SameLogin(oldMain, c.RTSPMainURL) || !urlcred.SameLogin(oldSub, c.RTSPSubURL)
	if req.Enabled != nil {
		c.Enabled = *req.Enabled
	}
	if req.HAPublish != nil {
		c.HAHidden = !*req.HAPublish
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
	if req.MotionDetection != nil {
		c.MotionDetection = *req.MotionDetection
	}
	if req.AudioDetection != nil {
		c.AudioDetection = *req.AudioDetection
	}
	if req.AudioThreshold != nil {
		c.AudioThreshold = *req.AudioThreshold
	}
	locationChanged := false
	if req.LocationID != nil {
		next := s.resolveLocationID(*req.LocationID)
		locationChanged = next != c.LocationID
		c.LocationID = next
	}
	if req.DetectMethod != nil {
		c.DetectMethod = validDetectMethod(*req.DetectMethod)
	}
	if req.TempSensor != nil {
		c.TempSensor = *req.TempSensor
	}
	if req.Vendor != nil {
		c.Vendor = validVendor(*req.Vendor)
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
	qualityBefore := [3]int{c.RecordHeight, c.RecordFPS, c.RecordKbps}
	if req.RecordHeight != nil {
		c.RecordHeight = *req.RecordHeight
	}
	if req.RecordFPS != nil {
		c.RecordFPS = *req.RecordFPS
	}
	if req.RecordKbps != nil {
		c.RecordKbps = *req.RecordKbps
	}
	qualityChanged := qualityBefore != [3]int{c.RecordHeight, c.RecordFPS, c.RecordKbps}
	if err := s.st.UpdateCamera(c); err != nil {
		writeStoreError(w, err)
		return
	}
	s.syncStreams()
	if locationChanged || (qualityChanged && s.rec != nil) {
		// New footage must land on the new location: cleanly restart this camera's
		// recorder so ffmpeg writes to (and the indexer scans) the new directory. A
		// quality change needs the same, since ffmpeg's arguments are set at start.
		s.rec.RestartCamera(c.ID)
	}
	detail := "mode=" + c.RecordMode
	if loginChanged {
		detail += " login=changed" // that it changed, never what to
	}
	if c.HAHidden != haBefore && s.ha != nil {
		if c.HAHidden {
			s.ha.ForgetCamera(c.ID)
		} else {
			s.ha.Republish()
		}
	}
	s.logAudit(r, "camera.update", c.Name, detail)
	v := cameraView(c, true)
	setETag(w, v)
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleDeleteCamera(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := s.st.GetCamera(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.precondition(w, r, etagOf(cameraView(c, true))) {
		return
	}
	// Reclaim the camera's on-disk footage and thumbnails BEFORE deleting the
	// row: DeleteCamera cascades away the segment/event rows that tell us where
	// those files live, so doing it first would orphan the bytes forever. (For a
	// camera on a remote location with a long history this is done synchronously
	// and may take a moment — acceptable for an admin delete.)
	s.purgeCameraFiles(id)
	if err := s.st.DeleteCamera(id); err != nil {
		writeStoreError(w, err)
		return
	}
	if s.ha != nil {
		s.ha.ForgetCamera(id)
	}
	s.syncStreams()
	s.logAudit(r, "camera.delete", c.Name, "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// syncStreams reconciles go2rtc with the current cameras in the background.
func (s *Server) syncStreams() {
	if s.sm == nil {
		return // streaming disabled (or under test) — nothing to reconcile
	}
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
		// Which cameras use object recognition may have changed with them; this
		// also reconciles motion detection.
		s.detect.ApplyConfig()
		s.det.Reconcile()
	}()
}

// cameraView renders a camera for the API. Its links never leave the server with
// their login: with links=true they carry it masked (urlcred.Redact), otherwise
// they are empty. The ETag is computed over this view too, so not even a hash of
// a password reaches a client.
func cameraView(c *store.Camera, links bool) map[string]any {
	mainURL, subURL := "", ""
	if links {
		mainURL, subURL = urlcred.Redact(c.RTSPMainURL), urlcred.Redact(c.RTSPSubURL)
	}
	return map[string]any{
		"id":                c.ID,
		"name":              c.Name,
		"rtspMainUrl":       mainURL,
		"rtspSubUrl":        subURL,
		"enabled":           c.Enabled,
		"recordMode":        c.RecordMode,
		"retentionDays":     c.RetentionDays,
		"storageQuotaBytes": c.StorageQuotaBytes,
		"motionThreshold":   c.MotionThreshold,
		"motionDetection":   c.MotionDetection,
		"audioDetection":    c.AudioDetection,
		"audioThreshold":    c.AudioThreshold,
		"locationId":        c.LocationID,
		"detectMethod":      c.DetectMethod,
		"tempSensor":        c.TempSensor,
		"vendor":            c.Vendor,
		"fps":               c.FPS,
		"sortOrder":         c.SortOrder,
		"prerollMs":         c.PrerollMs,
		"postrollMs":        c.PostrollMs,
		"cooldownMs":        c.CooldownMs,
		"minDurationMs":     c.MinDurationMs,
		"recordHeight":      c.RecordHeight,
		"recordFps":         c.RecordFPS,
		"recordKbps":        c.RecordKbps,
		"haPublish":         !c.HAHidden,
		"createdAtMs":       msFromSec(c.CreatedAt), // stored in seconds; wire is ms (DD-WIRE-TIME)
		"updatedAtMs":       msFromSec(c.UpdatedAt),
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
