package api

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/onvifclient"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Camera-side ONVIF: read and adjust the picture settings a camera exposes
// (brightness, contrast, saturation, sharpness, day/night), without leaving
// Aperture for the vendor's web UI.
//
// Strictly optional and non-disruptive: nothing here is consulted by the
// recorder, every call is made on demand with a short timeout, and a camera that
// is unreachable or refuses the account degrades to a clear message instead of
// an error state anywhere else.

// probeCache memoises the ONVIF discovery step. A probe walks candidate ports and
// makes several SOAP calls; doing that on every read made a spotlight toggle feel
// laggy and made polling for state changes too expensive to contemplate. The
// result only changes when the camera or its config does, so a short TTL is
// plenty — and any config write clears the entry outright.
type cachedProbe struct {
	res onvifclient.ProbeResult
	at  time.Time
}

var (
	probeMu    sync.Mutex
	probeCache = map[string]cachedProbe{}
)

const probeTTL = 2 * time.Minute

func cachedOnvifProbe(ctx context.Context, camID, host, user, pass string, ports []int) onvifclient.ProbeResult {
	probeMu.Lock()
	if c, ok := probeCache[camID]; ok && time.Since(c.at) < probeTTL && c.res.OK {
		probeMu.Unlock()
		return c.res
	}
	probeMu.Unlock()

	res := onvifclient.Probe(ctx, host, user, pass, ports, onvifTimeout)
	probeMu.Lock()
	probeCache[camID] = cachedProbe{res: res, at: time.Now()}
	probeMu.Unlock()
	return res
}

// invalidateProbe drops a camera's cached discovery (credentials/port changed).
func invalidateProbe(camID string) {
	probeMu.Lock()
	delete(probeCache, camID)
	probeMu.Unlock()
}

// onvifTimeout bounds a single camera call. Cameras on a busy LAN answer in
// well under a second; anything slower must not hold an API request open.
const onvifTimeout = 5 * time.Second

// onvifClientFor builds a client for a camera from its stored config, falling
// back to the credentials embedded in its RTSP URL — the zero-config path for
// cameras whose streaming account may also read ONVIF.
func (s *Server) onvifClientFor(camID string) (*onvifclient.Client, *store_ONVIF, error) {
	cam, err := s.st.GetCamera(camID)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := s.st.CameraONVIFConfig(camID)
	if err != nil {
		return nil, nil, err
	}
	host, rtspUser, rtspPass := onvifclient.HostFromRTSP(cam.RTSPMainURL)
	user, pass := cfg.User, ""
	if cfg.PassEnc != "" && s.cipher != nil {
		if plain, err := s.cipher.Decrypt(cfg.PassEnc); err == nil {
			pass = plain
		}
	}
	if user == "" {
		user, pass = rtspUser, rtspPass
	}
	return onvifclient.New("", user, pass, onvifTimeout), &store_ONVIF{cfg: cfg, host: host, user: user, pass: pass}, nil
}

// store_ONVIF carries the resolved connection details alongside the stored row.
type store_ONVIF struct {
	cfg  *store.CameraONVIF
	host string
	user string
	pass string
}

func (s *Server) handleGetCameraONVIF(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, res, err := s.onvifClientFor(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	out := map[string]any{
		"enabled": res.cfg.Enabled,
		"port":    res.cfg.Port,
		"host":    res.host,
		// hasUser / hasPassword tell the UI an account is stored without revealing
		// it: camera credentials never leave the server, only get replaced.
		"hasUser":     res.cfg.User != "",
		"hasPassword": res.cfg.PassEnc != "",
	}
	if !res.cfg.Enabled {
		// Not switched on: report config only. No camera is contacted, so opening a
		// camera's settings never blocks on an unreachable device.
		writeJSON(w, http.StatusOK, out)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	ports := onvifclient.DefaultPorts
	if res.cfg.Port > 0 {
		ports = []int{res.cfg.Port}
	}
	probe := cachedOnvifProbe(ctx, id, res.host, res.user, res.pass, ports)
	out["probe"] = probe
	if probe.OK && probe.HasImaging && probe.VideoSource != "" {
		c := onvifclient.New(hostPort(res.host, probe.Port), res.user, res.pass, onvifTimeout)
		if im, err := c.Imaging(ctx, probe.VideoSource); err == nil {
			out["imaging"] = im
		} else {
			out["imagingError"] = err.Error()
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSetCameraONVIF(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Enabled  *bool   `json:"enabled"`
		Port     *int    `json:"port"`
		User     *string `json:"user"`
		Password *string `json:"password"` // omitted = keep, "" = clear
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	cfg, err := s.st.CameraONVIFConfig(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}
	if req.Port != nil && *req.Port >= 0 && *req.Port <= 65535 {
		cfg.Port = *req.Port
	}
	if req.User != nil {
		cfg.User = *req.User
	}
	var passEnc *string
	if req.Password != nil {
		enc := ""
		if *req.Password != "" {
			if s.cipher == nil {
				writeError(w, http.StatusInternalServerError, "credential encryption unavailable")
				return
			}
			e, err := s.cipher.Encrypt(*req.Password)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "could not store the credential")
				return
			}
			enc = e
		}
		passEnc = &enc
	}
	if err := s.st.SetCameraONVIFConfig(id, cfg, passEnc); err != nil {
		writeStoreError(w, err)
		return
	}
	invalidateProbe(id) // credentials or port may have changed
	s.logAudit(r, "camera.onvif", id, "")
	s.handleGetCameraONVIF(w, r)
}

// handleSetCameraImaging applies picture settings to the camera itself.
func (s *Server) handleSetCameraImaging(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Brightness  *float64 `json:"brightness"`
		Contrast    *float64 `json:"contrast"`
		Saturation  *float64 `json:"saturation"`
		Sharpness   *float64 `json:"sharpness"`
		IrCutFilter string   `json:"irCutFilter"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	_, res, err := s.onvifClientFor(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !res.cfg.Enabled {
		writeErrorCode(w, http.StatusBadRequest, "validation", "camera settings are not switched on for this camera")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ports := onvifclient.DefaultPorts
	if res.cfg.Port > 0 {
		ports = []int{res.cfg.Port}
	}
	probe := cachedOnvifProbe(ctx, id, res.host, res.user, res.pass, ports)
	if !probe.OK || probe.VideoSource == "" {
		writeErrorCode(w, http.StatusBadGateway, "unreachable", firstNonEmptyStr(probe.Error, "the camera did not answer"))
		return
	}
	c := onvifclient.New(hostPort(res.host, probe.Port), res.user, res.pass, onvifTimeout)
	cur, err := c.Imaging(ctx, probe.VideoSource)
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, "unreachable", err.Error())
		return
	}
	next := &onvifclient.Imaging{
		Brightness: req.Brightness, Contrast: req.Contrast,
		Saturation: req.Saturation, Sharpness: req.Sharpness,
		IrCutFilter: req.IrCutFilter,
	}
	if err := c.SetImaging(ctx, probe.VideoSource, cur, next); err != nil {
		// The common real-world failure: the camera account can read but not write.
		// Say that plainly rather than "500".
		if err == onvifclient.ErrUnauthorized {
			writeErrorCode(w, http.StatusForbidden, "forbidden.camera",
				"this camera account may view settings but not change them — use an admin account on the camera")
			return
		}
		writeErrorCode(w, http.StatusBadGateway, "unreachable", err.Error())
		return
	}
	s.logAudit(r, "camera.imaging", id, "")

	// Report the values just applied rather than re-reading: like the spotlight,
	// a read issued immediately after a write can still return the old value and
	// would make a slider jump back under the operator's finger.
	merged := *cur
	if next.Brightness != nil {
		merged.Brightness = next.Brightness
	}
	if next.Contrast != nil {
		merged.Contrast = next.Contrast
	}
	if next.Saturation != nil {
		merged.Saturation = next.Saturation
	}
	if next.Sharpness != nil {
		merged.Sharpness = next.Sharpness
	}
	if next.IrCutFilter != "" {
		merged.IrCutFilter = next.IrCutFilter
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": res.cfg.Enabled, "port": res.cfg.Port, "user": res.cfg.User,
		"host": res.host, "hasPassword": res.cfg.PassEnc != "",
		"probe": probe, "imaging": merged,
	})
}

func hostPort(host string, port int) string {
	if port <= 0 {
		port = 80
	}
	return host + ":" + strconv.Itoa(port)
}

func firstNonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
