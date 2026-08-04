package api

import (
	"net/http"
	"strings"

	"github.com/TheOriUHD/aperture/internal/stream"
)

// handleGetHA returns the Home Assistant / MQTT configuration (without the
// password) plus the per-camera RTSP URLs to add for live video in HA.
func (s *Server) handleGetHA(w http.ResponseWriter, r *http.Request) {
	set := s.ha.LoadSettings()

	host := r.Host
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	cams, _ := s.st.ListCameras()
	streams := make([]map[string]any, 0, len(cams))
	for _, c := range cams {
		streams = append(streams, map[string]any{
			"cameraId": c.ID,
			"name":     c.Name,
			"rtsp":     "rtsp://" + host + ":8554/" + stream.StreamName(c.ID),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":     set.Enabled,
		"broker":      set.Broker,
		"username":    set.Username,
		"prefix":      set.Prefix,
		"sensorTopic": set.SensorTopic,
		"hasPassword": set.Password != "",
		"connected":   s.ha.Connected(),
		"streams":     streams,
	})
}

// handleHASensors returns the Home Assistant sensor readings Aperture has cached
// from MQTT — the pool a camera can pick a temperature sensor from.
func (s *Server) handleHASensors(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ha.Sensors())
}

// handleSetHA updates the HA/MQTT configuration (admin) and reconnects.
func (s *Server) handleSetHA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled     bool   `json:"enabled"`
		Broker      string `json:"broker"`
		Username    string `json:"username"`
		Password    string `json:"password"` // empty = keep existing
		Prefix      string `json:"prefix"`
		SensorTopic string `json:"sensorTopic"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	prefix := strings.TrimSpace(req.Prefix)
	if prefix == "" {
		prefix = "homeassistant"
	}
	sensorTopic := strings.Trim(strings.TrimSpace(req.SensorTopic), "/")
	if sensorTopic == "" {
		sensorTopic = "homeassistant/sensor"
	}
	_ = s.st.SetSetting("ha_enabled", boolStr(req.Enabled))
	_ = s.st.SetSetting("ha_broker", strings.TrimSpace(req.Broker))
	_ = s.st.SetSetting("ha_user", req.Username)
	_ = s.st.SetSetting("ha_prefix", prefix)
	_ = s.st.SetSetting("ha_sensor_topic", sensorTopic)
	if req.Password != "" {
		_ = s.st.SetSetting("ha_pass", req.Password)
	}
	s.ha.Reconcile()
	s.logAudit(r, "ha.update", "", "enabled="+boolStr(req.Enabled))
	s.handleGetHA(w, r)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
