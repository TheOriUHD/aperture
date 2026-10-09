package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/onvifclient"
	"github.com/TheOriUHD/aperture/internal/reolink"
	"github.com/TheOriUHD/aperture/internal/store"
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
	// An EXPLICIT marking is the operator's stated fact about the camera and outranks
	// probing: it holds while the camera is offline, while ONVIF is switched off, and
	// across unrelated settings edits. Those are exactly the cases where a probe-only
	// gate used to make working controls disappear with no explanation.
	if cam, cerr := s.st.GetCamera(camID); cerr == nil && cam.Vendor == store.VendorReolink {
		if res.host == "" {
			return nil, cam.Vendor, nil // marked, but no address to reach it on
		}
		return cachedReolink(camID, res.host, res.user, res.pass), cam.Vendor, nil
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

// Reading vendor state means talking to the CAMERA, not to Aperture. A Reolink
// serves very few concurrent sessions, and hammering its HTTP API while it is also
// serving the RTSP stream that recording depends on can wedge it — it keeps
// answering pings while its stream stops delivering. The UI is careful not to poll,
// but the UI is not the only possible caller, so the floor is enforced HERE: reads
// within vendorTTL of the last one are served from memory and never reach the
// camera. Writes go straight through (an operator pressing a light switch must not
// wait) and drop the entry so the next read reflects reality.
const vendorTTL = 10 * time.Second

type vendorSnapshot struct {
	at   time.Time
	body map[string]any
}

var (
	vendorCacheMu sync.Mutex
	vendorCache   = map[string]vendorSnapshot{}
)

func cachedVendorRead(camID string) (map[string]any, bool) {
	vendorCacheMu.Lock()
	defer vendorCacheMu.Unlock()
	v, ok := vendorCache[camID]
	if !ok || time.Since(v.at) > vendorTTL {
		return nil, false
	}
	return v.body, true
}

func storeVendorRead(camID string, body map[string]any) {
	vendorCacheMu.Lock()
	defer vendorCacheMu.Unlock()
	vendorCache[camID] = vendorSnapshot{at: time.Now(), body: body}
}

func invalidateVendorRead(camID string) {
	vendorCacheMu.Lock()
	defer vendorCacheMu.Unlock()
	delete(vendorCache, camID)
}

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
	if body, ok := cachedVendorRead(id); ok {
		writeJSON(w, http.StatusOK, body)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, vendor, err := s.reolinkFor(ctx, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	out := map[string]any{"vendor": vendor, "supported": c != nil}
	if c == nil {
		storeVendorRead(id, out)
		writeJSON(w, http.StatusOK, out)
		return
	}
	if sp, err := c.GetSpotlight(ctx); err == nil {
		out["spotlight"] = sp
	} else {
		_, msg := vendorFailure("read the spotlight", err)
		out["error"] = msg
	}
	if ir, err := c.GetIR(ctx); err == nil {
		out["ir"] = ir
	}
	storeVendorRead(id, out)
	writeJSON(w, http.StatusOK, out)
}

// vendorFailure words a failed vendor call for the operator. A refusal (the camera
// answered and said no) and an unreachable camera need different fixes, so they get
// different codes, and a refusal carries the camera's reason instead of being
// flattened into "unreachable".
func vendorFailure(what string, err error) (code, msg string) {
	var ae *reolink.APIError
	if errors.As(err, &ae) {
		return "camera.refused", "The camera refused to " + what + ": " + ae.Explain() + "."
	}
	return "unreachable", "Could not reach the camera to " + what + ": " + err.Error() + "."
}

// writeVendorFailure reports a failed vendor call and logs it. The UI shows it only
// briefly, so the journal is where it can still be found afterwards.
func (s *Server) writeVendorFailure(w http.ResponseWriter, camID, what string, err error) {
	code, msg := vendorFailure(what, err)
	s.log.Warn("vendor: camera call failed", "camera", camID, "action", what, "code", code, "err", err)
	writeErrorCode(w, http.StatusBadGateway, code, msg)
}

func (s *Server) handleSetCameraVendor(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// Whatever happens below, the cached read is now stale.
	defer invalidateVendorRead(id)
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
	// Validate before any camera is contacted, so a bad value can never leave half
	// of a combined request applied.
	if req.IR != nil && *req.IR != "Auto" && *req.IR != "Off" {
		writeErrorCode(w, http.StatusBadRequest, "validation", "night vision must be Auto or Off")
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
	// The response reports what was APPLIED, not an immediate read-back. These
	// cameras answer a read issued straight after a write with the PREVIOUS value
	// (verified: state reads 0 for about a second after being set to 1), so
	// echoing that read made the UI snap back to the old state before the next
	// poll corrected it. The camera accepted the command; the poll a few seconds
	// later is what proves it stuck.
	applied := map[string]any{}
	if req.Spotlight != nil {
		cur, err := c.GetSpotlight(ctx)
		if err != nil {
			s.writeVendorFailure(w, id, "read the spotlight", err)
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
			what := "change the spotlight"
			switch {
			case req.Spotlight.State != nil && *req.Spotlight.State == 1:
				what = "turn the spotlight on"
			case req.Spotlight.State != nil:
				what = "turn the spotlight off"
			case req.Spotlight.Bright != nil:
				what = "change the spotlight brightness"
			}
			s.writeVendorFailure(w, id, what, err)
			return
		}
		applied["spotlight"] = next
	}
	if req.IR != nil {
		if err := c.SetIR(ctx, *req.IR); err != nil {
			s.writeVendorFailure(w, id, "change night vision", err)
			return
		}
		applied["ir"] = map[string]any{"state": *req.IR}
	}
	s.logAudit(r, "camera.vendor", id, "")

	out := map[string]any{"vendor": "REOLINK", "supported": true}
	for k, v := range applied {
		out[k] = v
	}
	// Fill in whichever half wasn't touched so the client keeps a complete view.
	if _, ok := out["spotlight"]; !ok {
		if sp, err := c.GetSpotlight(ctx); err == nil {
			out["spotlight"] = sp
		}
	}
	if _, ok := out["ir"]; !ok {
		if ir, err := c.GetIR(ctx); err == nil {
			out["ir"] = ir
		}
	}
	writeJSON(w, http.StatusOK, out)
}
