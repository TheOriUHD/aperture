package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/onvifclient"
	"github.com/TheOriUHD/aperture/internal/reolink"
)

// Vendor extras: controls a camera exposes through its OWN API that ONVIF does
// not model — currently Reolink's LED spotlight and IR illuminators.
//
// Gated on the camera having been identified as that vendor by the ONVIF probe,
// so it can never appear for a camera it doesn't apply to. Same posture as the
// rest of the ONVIF work: opt-in, short timeouts, off the recording path.

func (s *Server) reolinkFor(ctx context.Context, camID string) (*reolink.Client, string, error) {
	_, res, err := s.onvifClientFor(camID)
	if err != nil {
		return nil, "", err
	}
	if !res.cfg.Enabled {
		return nil, "", nil
	}
	ports := onvifclient.DefaultPorts
	if res.cfg.Port > 0 {
		ports = []int{res.cfg.Port}
	}
	probe := cachedOnvifProbe(ctx, camID, res.host, res.user, res.pass, ports)
	if !probe.OK || !strings.EqualFold(probe.Device.Manufacturer, "REOLINK") {
		return nil, probe.Device.Manufacturer, nil
	}
	// The vendor API lives on the camera's plain HTTP port, not the ONVIF one.
	// The client is cached per camera so its login token is reused across calls
	// (a fresh client would log in every single time).
	return cachedReolink(camID, res.host, res.user, res.pass), probe.Device.Manufacturer, nil
}

var (
	reolinkMu      sync.Mutex
	reolinkClients = map[string]*reolink.Client{}
)

// cachedReolink returns a per-camera client, rebuilt when the endpoint or login
// changes so a credential edit takes effect immediately.
func cachedReolink(camID, host, user, pass string) *reolink.Client {
	key := camID + "|" + host + "|" + user + "|" + pass
	reolinkMu.Lock()
	defer reolinkMu.Unlock()
	if c, ok := reolinkClients[key]; ok {
		return c
	}
	c := reolink.New(host, user, pass, 6*time.Second)
	// Drop any client for this camera under an older key.
	for k := range reolinkClients {
		if strings.HasPrefix(k, camID+"|") {
			delete(reolinkClients, k)
		}
	}
	reolinkClients[key] = c
	return c
}

func (s *Server) handleGetCameraVendor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, vendor, err := s.reolinkFor(ctx, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	out := map[string]any{"vendor": vendor, "supported": c != nil}
	if c == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	if sp, err := c.GetSpotlight(ctx); err == nil {
		out["spotlight"] = sp
	} else {
		out["error"] = err.Error()
	}
	if ir, err := c.GetIR(ctx); err == nil {
		out["ir"] = ir
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSetCameraVendor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Spotlight *struct {
			State  *int `json:"state"`
			Mode   *int `json:"mode"`
			Bright *int `json:"bright"`
		} `json:"spotlight"`
		IR *string `json:"ir"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c, _, err := s.reolinkFor(ctx, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if c == nil {
		writeErrorCode(w, http.StatusBadRequest, "validation", "this camera has no vendor controls available")
		return
	}
	if req.Spotlight != nil {
		cur, err := c.GetSpotlight(ctx)
		if err != nil {
			writeErrorCode(w, http.StatusBadGateway, "unreachable", err.Error())
			return
		}
		next := *cur
		if req.Spotlight.State != nil {
			next.State = *req.Spotlight.State
			// Turning it on by hand means "on now", not "wait for motion" — mode 0
			// with state 1 is how the camera models a manual override.
			if next.State == 1 && next.Mode == 0 {
				next.Mode = 0
			}
		}
		if req.Spotlight.Mode != nil {
			next.Mode = *req.Spotlight.Mode
		}
		if req.Spotlight.Bright != nil {
			next.Bright = *req.Spotlight.Bright
		}
		if err := c.SetSpotlight(ctx, next); err != nil {
			writeErrorCode(w, http.StatusBadGateway, "unreachable", err.Error())
			return
		}
	}
	if req.IR != nil {
		if err := c.SetIR(ctx, *req.IR); err != nil {
			writeErrorCode(w, http.StatusBadGateway, "unreachable", err.Error())
			return
		}
	}
	s.logAudit(r, "camera.vendor", id, "")
	s.handleGetCameraVendor(w, r)
}
