package api

import (
	"net/http"

	"github.com/TheOriUHD/aperture/internal/detect"
	"github.com/TheOriUHD/aperture/internal/site"
	"github.com/TheOriUHD/aperture/internal/version"
)

// apiVersions is the set of contract versions this build serves.
var apiVersions = []string{"v1"}

// handleAPIVersion is the public, pre-auth version-negotiation endpoint: enough
// for a client to decide whether it speaks this server's contract, but no
// subsystem topology (that is reconnaissance-worth and requires auth — see
// handleCapabilities).
func (s *Server) handleAPIVersion(w http.ResponseWriter, r *http.Request) {
	sv, _ := s.st.SchemaVersion()
	writeJSON(w, http.StatusOK, map[string]any{
		"apiVersions":   apiVersions,
		"serverVersion": version.Version,
		"schemaVersion": sv,
	})
}

// handleCapabilities reports which optional subsystems and contract features are
// live, so an extension can degrade gracefully instead of breaking on every
// Aperture update. Authenticated with any credential (no scope) — subsystem
// topology is gated, the bare version above is not.
func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	sv, _ := s.st.SchemaVersion()

	audio := false
	if cams, err := s.st.ListCameras(); err == nil {
		for _, c := range cams {
			if c.AudioDetection {
				audio = true
				break
			}
		}
	}
	detection := false
	if s.detect != nil {
		detection = s.detect.LoadConfig().Mode != detect.ModeOff
	}
	haEnabled, _ := s.st.GetSetting("ha_enabled")

	// The freeze marker (DD-FREEZE): once set, v1 is frozen and breaking changes
	// require v2. Empty until the first non-first-party client exists.
	frozenAt, _ := s.st.GetSetting("api.v1.frozen_at")

	// Runtime switch (T2.2): when true, the playback client derives seek time
	// from the HLS EXT-X-PROGRAM-DATE-TIME tags instead of /segments math.
	// Default off; flipped via PUT /system/playback with no redeploy.
	pdtSeek, _ := s.st.GetSetting("playback.pdt_seek")

	writeJSON(w, http.StatusOK, map[string]any{
		"apiVersions":   apiVersions,
		"serverVersion": version.Version,
		"schemaVersion": sv,
		"subsystems": map[string]bool{
			"detection":     detection,
			"homeAssistant": haEnabled == "true",
			"homeKit":       s.cfg.HomeKitEnabled,
			"onvif":         s.cfg.ONVIFEnabled,
			"weather":       site.Load(s.st).Configured,
			"audio":         audio,
		},
		// Contract features an extension can rely on. Flipped to true as each v1
		// capability lands (Phase 2), so a client can feature-detect rather than
		// assume.
		"features": map[string]bool{
			"cursorPagination": true, // T2.4 (Phase 2.A) — live
			"changeFeed":       true, // T2.5 (Phase 2.C) — live
			"coverageEndpoint": true,  // T2.1 (Phase 2.A) — live
			"idempotencyKey":   true,  // T1.9 (export/shares) — live
			"etagIfMatch":      false, // 1.9 ETag half not yet implemented (deferred)
			"pdtSeek":          pdtSeek == "true", // T2.2 runtime switch (default off)
		},
		"deprecations": []map[string]any{
			{"surface": "/api", "successor": "/api/v1", "sunset": nil},
		},
		"apiV1FrozenAt": frozenAt, // "" until the contract is frozen
	})
}
