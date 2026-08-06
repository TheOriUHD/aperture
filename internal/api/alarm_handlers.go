package api

import "net/http"

// Alarm (armed) state (3.4). Previously togglable only from Home Assistant and
// HomeKit; v1 exposes in-app control at GET/PUT /alarm (alarm:arm). "armed" is
// the same setting those integrations drive; the default (unset) is armed, so
// only an explicit "false" disarms.

func armedState(v string) bool { return v != "false" }

func (s *Server) handleGetAlarm(w http.ResponseWriter, r *http.Request) {
	v, _ := s.st.GetSetting("armed")
	writeJSON(w, http.StatusOK, map[string]any{"armed": armedState(v)})
}

func (s *Server) handleSetAlarm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Armed *bool `json:"armed"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Armed == nil {
		writeErrorCode(w, http.StatusBadRequest, "validation", "armed (boolean) is required")
		return
	}
	val := "true"
	if !*req.Armed {
		val = "false"
	}
	if err := s.st.SetSetting("armed", val); err != nil {
		writeError(w, http.StatusInternalServerError, "could not update alarm state")
		return
	}
	s.logAudit(r, "alarm.arm", val, "armed state changed via API")
	if s.notify != nil {
		s.notify.OnAlarmState(*req.Armed)
	}
	writeJSON(w, http.StatusOK, map[string]any{"armed": *req.Armed})
}
