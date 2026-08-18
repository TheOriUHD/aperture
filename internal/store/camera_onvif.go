package store

import "time"

// Per-camera ONVIF client config. Deliberately read and written through its own
// small queries rather than being folded into the Camera struct and cameraSelect:
// every camera read sits on the recording path, and this feature must not be able
// to disturb it.

// CameraONVIF is a camera's ONVIF connection config. PassEnc is ciphertext; the
// plaintext never leaves the API layer's decrypt step.
type CameraONVIF struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"` // 0 = auto-discover the ONVIF port
	User    string `json:"user"` // empty = reuse the RTSP URL's credentials
	PassEnc string `json:"-"`
}

// CameraONVIFConfig reads one camera's ONVIF config.
func (s *Store) CameraONVIFConfig(cameraID string) (*CameraONVIF, error) {
	var c CameraONVIF
	var enabled int
	err := s.db.QueryRow(
		`SELECT onvif_enabled, onvif_port, onvif_user, onvif_pass_enc FROM cameras WHERE id = ?`,
		cameraID).Scan(&enabled, &c.Port, &c.User, &c.PassEnc)
	if err != nil {
		return nil, err
	}
	c.Enabled = enabled == 1
	return &c, nil
}

// SetCameraONVIFConfig stores the config. A nil passEnc leaves the stored
// credential untouched, so saving other fields never wipes the password.
func (s *Store) SetCameraONVIFConfig(cameraID string, c *CameraONVIF, passEnc *string) error {
	enabled := 0
	if c.Enabled {
		enabled = 1
	}
	if passEnc != nil {
		return affected(s.db.Exec(
			`UPDATE cameras SET onvif_enabled = ?, onvif_port = ?, onvif_user = ?, onvif_pass_enc = ?, updated_at = ?
             WHERE id = ?`,
			enabled, c.Port, c.User, *passEnc, time.Now().Unix(), cameraID))
	}
	return affected(s.db.Exec(
		`UPDATE cameras SET onvif_enabled = ?, onvif_port = ?, onvif_user = ?, updated_at = ? WHERE id = ?`,
		enabled, c.Port, c.User, time.Now().Unix(), cameraID))
}
