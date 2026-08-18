package api

import (
	"net/http"
	"sync"
)

// ONVIF and HomeKit are optional interop surfaces. They used to be gated ONLY by
// an environment variable read once at boot, so turning either on meant editing
// compose and recreating the container. They are now settings-backed and driven
// from the UI: the setting is the source of truth, seeded from the env var on
// first boot so an existing env-configured deployment keeps its behaviour.
//
// Toggling starts or stops the subsystem in place (see cmd/nvrd/main.go's
// interopService) — no restart, and the reported state is the ACTUAL running
// state, never just what was requested.

const (
	settingONVIFEnabled   = "onvif.enabled"
	settingHomeKitEnabled = "homekit.enabled"
)

// InteropService is one togglable subsystem. main.go supplies implementations
// that start/stop the real ONVIF server and HomeKit bridge.
type InteropService interface {
	SetEnabled(bool) error
	Running() bool
	// Info returns display detail for the UI (address, pairing pin, …). Values
	// are safe to show: no secrets.
	Info() map[string]any
}

// SetInterop wires the togglable ONVIF/HomeKit services. Either may be nil (the
// UI then reports that subsystem as unavailable rather than pretending).
func (s *Server) SetInterop(onvif, homekit InteropService) {
	s.interopMu.Lock()
	defer s.interopMu.Unlock()
	s.onvifSvc = onvif
	s.homekitSvc = homekit
}

type interopHolder struct {
	interopMu  sync.Mutex
	onvifSvc   InteropService
	homekitSvc InteropService
}

func (s *Server) interopSvcs() (InteropService, InteropService) {
	s.interopMu.Lock()
	defer s.interopMu.Unlock()
	return s.onvifSvc, s.homekitSvc
}

func (s *Server) interopView() map[string]any {
	onvifSvc, hkSvc := s.interopSvcs()
	view := func(svc InteropService, want bool) map[string]any {
		m := map[string]any{
			"available": svc != nil,
			"enabled":   want,
			// running is the truth from the subsystem itself, so a failed start
			// shows as enabled-but-not-running instead of a false green light.
			"running": svc != nil && svc.Running(),
		}
		if svc != nil {
			for k, v := range svc.Info() {
				m[k] = v
			}
		}
		return m
	}
	return map[string]any{
		"onvif":   view(onvifSvc, s.st.GetSettingBool(settingONVIFEnabled)),
		"homekit": view(hkSvc, s.st.GetSettingBool(settingHomeKitEnabled)),
	}
}

func (s *Server) handleGetInterop(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.interopView())
}

func (s *Server) handleSetInterop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ONVIF   *bool `json:"onvif"`
		HomeKit *bool `json:"homekit"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	onvifSvc, hkSvc := s.interopSvcs()

	apply := func(want *bool, key string, svc InteropService, name string) error {
		if want == nil {
			return nil
		}
		if svc == nil {
			return nil // not built into this binary; the view reports unavailable
		}
		if err := svc.SetEnabled(*want); err != nil {
			s.log.Error("interop toggle failed", "service", name, "enabled", *want, "err", err)
			return err
		}
		return s.st.SetSettingBool(key, *want)
	}
	if err := apply(req.ONVIF, settingONVIFEnabled, onvifSvc, "onvif"); err != nil {
		writeError(w, http.StatusInternalServerError, "could not start ONVIF: "+err.Error())
		return
	}
	if err := apply(req.HomeKit, settingHomeKitEnabled, hkSvc, "homekit"); err != nil {
		writeError(w, http.StatusInternalServerError, "could not start HomeKit: "+err.Error())
		return
	}
	s.logAudit(r, "system.interop", "", "")
	s.emitConfig("interop")
	writeJSON(w, http.StatusOK, s.interopView())
}
