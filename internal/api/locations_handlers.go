package api

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/TheOriUHD/aperture/internal/storage"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Storage-location handlers (admin). Phase 1 supports local destinations only;
// the built-in default location (id "") is virtual — it maps to the current
// recordings dir + storage_cap_bytes and is injected into list responses rather
// than stored, so existing footage and the reserve wizard keep working unchanged.

type locationReq struct {
	Name            string         `json:"name"`
	Type            string         `json:"type"`
	Config          map[string]any `json:"config"`
	Secret          string         `json:"secret"` // plaintext credential (encrypted before storage)
	AllocationBytes int64          `json:"allocationBytes"`
	SortOrder       int            `json:"sortOrder"`
}

type locationUpdateReq struct {
	Name            *string        `json:"name"`
	AllocationBytes *int64         `json:"allocationBytes"`
	Enabled         *bool          `json:"enabled"`
	Config          map[string]any `json:"config"` // network reconfig (nil = unchanged)
	Secret          *string        `json:"secret"` // nil = unchanged, "" = clear
}

func isNetworkType(t string) bool { return t == store.LocationSMB || t == store.LocationSFTP }

func configPath(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	p, _ := cfg["path"].(string)
	return strings.TrimSpace(p)
}

// resolveLocationID returns the location id to persist for a camera: "" for the
// default, an existing location's id, or "" (fall back to default) if it doesn't
// exist — so camera rows never point at a ghost location.
func (s *Server) resolveLocationID(id string) string {
	if id == store.DefaultLocationID {
		return store.DefaultLocationID
	}
	if _, err := s.st.GetStorageLocation(id); err != nil {
		return store.DefaultLocationID
	}
	return id
}

// defaultLocationView describes the virtual built-in location the same shape as a
// configured one, so the UI can render them uniformly.
func (s *Server) defaultLocationView() map[string]any {
	dir := s.recordingsDir()
	used, _ := s.st.TotalBytesInLocation(store.DefaultLocationID)
	used += s.appFilesBytes()
	camCount, _ := s.st.CamerasUsingLocation(store.DefaultLocationID)
	total, free := diskUsage(dir)
	return map[string]any{
		"id":              store.DefaultLocationID,
		"name":            "Primary storage",
		"type":            store.LocationLocal,
		"config":          map[string]any{"path": dir},
		"allocationBytes": s.st.GetSettingInt64("storage_cap_bytes"),
		"enabled":         true,
		"sortOrder":       -1,
		"hasCredential":   false,
		"isDefault":       true,
		"usedBytes":       used,
		"cameraCount":     camCount,
		"diskTotalBytes":  total,
		"diskFreeBytes":   free,
	}
}

// statWithTimeout runs a backend's Stat with a hard deadline so an unreachable
// share can't block a request. The worker goroutine resolves on its own once the
// underlying connection times out (the buffered channel keeps it from leaking).
func statWithTimeout(b storage.Backend, d time.Duration) (storage.Stat, bool) {
	ch := make(chan storage.Stat, 1)
	go func() {
		st, err := b.Stat()
		if err != nil {
			ch <- storage.Stat{}
			return
		}
		ch <- st
	}()
	select {
	case st := <-ch:
		return st, st.Supported
	case <-time.After(d):
		return storage.Stat{}, false
	}
}

func (s *Server) locationView(loc *store.StorageLocation) map[string]any {
	used, _ := s.st.TotalBytesInLocation(loc.ID)
	camCount, _ := s.st.CamerasUsingLocation(loc.ID)
	m := map[string]any{
		"id":              loc.ID,
		"name":            loc.Name,
		"type":            loc.Type,
		"config":          loc.Config,
		"allocationBytes": loc.AllocationBytes,
		"enabled":         loc.Enabled,
		"sortOrder":       loc.SortOrder,
		"hasCredential":   loc.HasCredential,
		"isDefault":       false,
		"usedBytes":       used,
		"cameraCount":     camCount,
		"createdAt":       loc.CreatedAt,
		"updatedAt":       loc.UpdatedAt,
	}
	if loc.Type == store.LocationLocal {
		if p := configPath(loc.Config); p != "" {
			total, free := diskUsage(p)
			m["diskTotalBytes"] = total
			m["diskFreeBytes"] = free
		}
	} else if isNetworkType(loc.Type) && loc.Enabled && s.registry != nil {
		// Live capacity from the share, bounded so a slow/down destination can't
		// stall this (polled) endpoint.
		if b, err := s.registry.For(loc.ID); err == nil {
			if st, ok := statWithTimeout(b, 2500*time.Millisecond); ok && st.Total > 0 {
				m["diskTotalBytes"] = st.Total
				m["diskFreeBytes"] = st.Free
			}
		}
	}
	return m
}

// allLocationViews returns the default location followed by every configured one,
// each with live usage stats. Shared by the storage overview and the locations API.
func (s *Server) allLocationViews() []map[string]any {
	locs, _ := s.st.ListStorageLocations()
	out := make([]map[string]any, 0, len(locs)+1)
	out = append(out, s.defaultLocationView())
	for _, l := range locs {
		out = append(out, s.locationView(l))
	}
	return out
}

func (s *Server) handleListLocations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"locations": s.allLocationViews(),
		"dataDir":   s.cfg.DataDir, // fixed install location, shown read-only
	})
}

func (s *Server) handleCreateLocation(w http.ResponseWriter, r *http.Request) {
	var req locationReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "a name is required")
		return
	}
	alloc := req.AllocationBytes
	if alloc < 0 {
		alloc = 0
	}

	var config map[string]any
	var auditDetail string
	switch req.Type {
	case store.LocationLocal, "":
		path := configPath(req.Config)
		if path == "" {
			writeError(w, http.StatusBadRequest, "a folder path is required")
			return
		}
		clean := filepath.Clean(path)
		if clean == filepath.Clean(s.cfg.DataDir) || clean == filepath.Clean(s.recordingsDir()) {
			writeError(w, http.StatusBadRequest, "choose a different folder from the app and default storage")
			return
		}
		if err := checkWritableDir(path); err != nil {
			writeError(w, http.StatusBadRequest, "folder not usable: "+err.Error())
			return
		}
		config = map[string]any{"path": clean}
		auditDetail = "local " + clean
	case store.LocationSMB, store.LocationSFTP:
		if s.cipher == nil && req.Secret != "" {
			writeError(w, http.StatusServiceUnavailable, "credential encryption is unavailable")
			return
		}
		config = sanitizeNetConfig(req.Config)
		if cfgStrVal(config, "host") == "" {
			writeError(w, http.StatusBadRequest, "a host is required")
			return
		}
		if req.Type == store.LocationSMB && cfgStrVal(config, "share") == "" {
			writeError(w, http.StatusBadRequest, "a share name is required")
			return
		}
		auditDetail = req.Type + " " + cfgStrVal(config, "host")
	default:
		writeError(w, http.StatusBadRequest, "unknown storage type")
		return
	}

	loc := &store.StorageLocation{
		Name:            name,
		Type:            defaultStr(req.Type, store.LocationLocal),
		Config:          config,
		AllocationBytes: alloc,
		Enabled:         true,
		SortOrder:       req.SortOrder,
	}
	if err := s.st.CreateStorageLocation(loc); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create location")
		return
	}
	// Encrypt + store the credential separately (never in config_json).
	if isNetworkType(loc.Type) && req.Secret != "" && s.cipher != nil {
		if enc, err := s.cipher.Encrypt(req.Secret); err == nil {
			_ = s.st.SetLocationSecret(loc.ID, enc)
			loc.HasCredential = true
		}
	}
	if s.registry != nil {
		s.registry.Invalidate(loc.ID)
	}
	s.logAudit(r, "storage.location.create", name, auditDetail)
	writeJSON(w, http.StatusCreated, s.locationView(loc))
}

// sanitizeNetConfig keeps only the recognised non-secret fields for a network
// location, so a stray "password"/"secret" key can never be persisted in config.
func sanitizeNetConfig(in map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"host", "port", "share", "subfolder", "user", "domain", "path", "authMode", "hostKey"} {
		if v, ok := in[k]; ok {
			out[k] = v
		}
	}
	return out
}

func cfgStrVal(cfg map[string]any, key string) string {
	v, _ := cfg[key].(string)
	return strings.TrimSpace(v)
}

func (s *Server) handleUpdateLocation(w http.ResponseWriter, r *http.Request) {
	loc, err := s.st.GetStorageLocation(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var req locationUpdateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		loc.Name = strings.TrimSpace(*req.Name)
	}
	if req.AllocationBytes != nil {
		if *req.AllocationBytes < 0 {
			loc.AllocationBytes = 0
		} else {
			loc.AllocationBytes = *req.AllocationBytes
		}
	}
	if req.Enabled != nil {
		loc.Enabled = *req.Enabled
	}
	// Network reconfig: merge recognised non-secret fields (path of a local
	// location is intentionally immutable — existing segments hold absolute paths).
	if req.Config != nil && isNetworkType(loc.Type) {
		merged := sanitizeNetConfig(req.Config)
		if cfgStrVal(merged, "host") != "" {
			loc.Config = merged
		}
	}
	if err := s.st.UpdateStorageLocation(loc); err != nil {
		writeStoreError(w, err)
		return
	}
	// Credential change (network only): nil = leave as-is, "" = clear, else re-encrypt.
	if req.Secret != nil && isNetworkType(loc.Type) {
		if *req.Secret == "" {
			_ = s.st.SetLocationSecret(loc.ID, "")
		} else if s.cipher != nil {
			if enc, err := s.cipher.Encrypt(*req.Secret); err == nil {
				_ = s.st.SetLocationSecret(loc.ID, enc)
			}
		}
	}
	if s.registry != nil {
		s.registry.Invalidate(loc.ID)
	}
	s.logAudit(r, "storage.location.update", loc.Name, "")
	writeJSON(w, http.StatusOK, s.locationView(loc))
}

func (s *Server) handleDeleteLocation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	loc, err := s.st.GetStorageLocation(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	force := r.URL.Query().Get("force") == "true"
	camCount, _ := s.st.CamerasUsingLocation(id)
	segCount, _ := s.st.CountSegmentsInLocation(id)
	if !force && (camCount > 0 || segCount > 0) {
		// Refuse: this would orphan cameras or footage. The client can reassign +
		// purge first, or retry with ?force=true to tear it down.
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":        "location in use",
			"cameraCount":  camCount,
			"segmentCount": segCount,
		})
		return
	}
	if force {
		// Detach the disk: move any cameras still targeting it back to the default
		// (atomically, so an interrupt can't leave some reassigned and some not),
		// restart their recorders so they stop writing here, then delete the
		// footage while the backend is still resolvable.
		moved, err := s.st.ReassignCameras(id, store.DefaultLocationID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		for _, cid := range moved {
			s.rec.RestartCamera(cid)
		}
		s.purgeLocationSegments(id)
	}
	if err := s.st.DeleteStorageLocation(id); err != nil {
		writeStoreError(w, err)
		return
	}
	// Drop the cached backend handle (an open SMB/SFTP connection or resolved
	// local root) for the now-deleted location. Create and update both do this;
	// delete previously forgot, leaving a stale connection and cache entry that a
	// later location reusing the same id could pick up. Runs for empty locations
	// too — a probe/test may have opened a connection.
	s.registry.Invalidate(id)
	s.logAudit(r, "storage.location.delete", loc.Name, "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// purgeLocationSegments removes a location's segment files and index rows. Locked
// (protected/exported) segments are left in place.
func (s *Server) purgeLocationSegments(locationID string) {
	for {
		segs, err := s.st.OldestSegmentsInLocation(locationID, 200)
		if err != nil || len(segs) == 0 {
			return
		}
		for _, seg := range segs {
			s.removeSegmentFile(seg)
			_ = s.st.DeleteSegment(seg.ID)
		}
		if len(segs) < 200 {
			return
		}
	}
}

// handleTestLocation runs a destination's connection checklist without saving it,
// so the setup wizard can show a live "dial → verify → probe → capacity" sequence.
// Local folders and network shares (SMB/SFTP) are all probed for real.
func (s *Server) handleTestLocation(w http.ResponseWriter, r *http.Request) {
	var req locationReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Local: guard against aliasing the app data or default recordings dir before
	// probing (their accounting/reservations would collide).
	if req.Type == "" || req.Type == store.LocationLocal {
		clean := filepath.Clean(configPath(req.Config))
		if clean == filepath.Clean(s.cfg.DataDir) || clean == filepath.Clean(s.recordingsDir()) {
			writeJSON(w, http.StatusOK, storage.ProbeResult{OK: false, Steps: []storage.Step{{
				ID: "path", Label: "Check the folder path", Status: storage.StepFail,
				Detail: "Choose a different folder from the app and default storage.",
			}}})
			return
		}
	}
	res := storage.Probe(&storage.Spec{Type: req.Type, Config: req.Config, Secret: req.Secret})
	writeJSON(w, http.StatusOK, res)
}

// handleAllocateLocation sets a destination's allocation and applies it
// immediately (no restart): the default location's hard-reserve cap
// (storage_cap_bytes, clamped to what the disk can hold) or a configured
// location's cap. One control for both, so local hard-reserve and network soft
// caps are set the same way.
func (s *Server) handleAllocateLocation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LocationID      string `json:"locationId"`
		AllocationBytes int64  `json:"allocationBytes"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AllocationBytes < 0 {
		req.AllocationBytes = 0
	}
	if req.LocationID == store.DefaultLocationID {
		// Clamp to the ceiling the local disk can actually reserve (leaves margin).
		if bd := s.diskBreakdown(); bd != nil {
			if maxAlloc, ok := bd["maxAllocatableBytes"].(int64); ok && req.AllocationBytes > maxAlloc {
				req.AllocationBytes = maxAlloc
			}
		}
		_ = s.st.SetSetting("storage_cap_bytes", strconv.FormatInt(req.AllocationBytes, 10))
	} else {
		loc, err := s.st.GetStorageLocation(req.LocationID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		loc.AllocationBytes = req.AllocationBytes
		if err := s.st.UpdateStorageLocation(loc); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	s.triggerReconcile()
	s.logAudit(r, "storage.allocate", req.LocationID, strconv.FormatInt(req.AllocationBytes, 10))
	s.emitStorage()
	writeJSON(w, http.StatusOK, map[string]any{"locations": s.allLocationViews(), "dataDir": s.cfg.DataDir})
}

func (s *Server) handleReorderLocations(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.st.ReorderStorageLocations(req.IDs); err != nil {
		writeError(w, http.StatusInternalServerError, "could not reorder locations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleAssignCameras(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LocationID string   `json:"locationId"`
		CameraIDs  []string `json:"cameraIds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Validate the target exists (or is the default) before moving anything.
	if req.LocationID != store.DefaultLocationID {
		if _, err := s.st.GetStorageLocation(req.LocationID); err != nil {
			writeError(w, http.StatusBadRequest, "unknown location")
			return
		}
	}
	moved := 0
	for _, cid := range req.CameraIDs {
		cam, err := s.st.GetCamera(cid)
		if err != nil || cam.LocationID == req.LocationID {
			continue
		}
		if err := s.st.SetCameraLocation(cid, req.LocationID); err != nil {
			continue
		}
		s.rec.RestartCamera(cid)
		moved++
	}
	s.logAudit(r, "storage.location.assign", req.LocationID, "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "moved": moved})
}
