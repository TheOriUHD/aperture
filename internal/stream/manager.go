// Package stream orchestrates go2rtc: it pushes camera sources into go2rtc so a
// single upstream connection per camera is fanned out to recorders, browsers,
// and Home Assistant, and it proxies go2rtc's live endpoints behind Aperture's
// own auth.
package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// Manager talks to a go2rtc instance over its HTTP API.
type Manager struct {
	apiBase   string // e.g. http://go2rtc:1984
	rtspBase  string // e.g. rtsp://go2rtc:8554
	http      *http.Client
	proxy     *httputil.ReverseProxy
	proxyHost string
}

// NewManager builds a Manager. apiBase is the go2rtc HTTP API; rtspBase is its
// RTSP restream base used for recorder + Home Assistant URLs.
func NewManager(apiBase, rtspBase string) (*Manager, error) {
	u, err := url.Parse(apiBase)
	if err != nil {
		return nil, fmt.Errorf("parse go2rtc url: %w", err)
	}
	rp := httputil.NewSingleHostReverseProxy(u)
	// Preserve the host the proxy dials; default director already rewrites it.
	return &Manager{
		apiBase:   apiBase,
		rtspBase:  rtspBase,
		http:      &http.Client{Timeout: 10 * time.Second},
		proxy:     rp,
		proxyHost: u.Host,
	}, nil
}

// StreamName is the go2rtc stream identifier for a camera.
func StreamName(cameraID string) string { return "cam_" + cameraID }

// RTSPURL is the internal restream URL recorders + Home Assistant consume.
func (m *Manager) RTSPURL(cameraID string) string {
	return fmt.Sprintf("%s/%s", m.rtspBase, StreamName(cameraID))
}

// Sync reconciles go2rtc's streams with the given cameras: enabled cameras get
// their sources registered, others are removed.
func (m *Manager) Sync(ctx context.Context, cameras []*store.Camera) error {
	existing, err := m.streams(ctx)
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, c := range cameras {
		if !c.Enabled || c.RTSPMainURL == "" {
			continue
		}
		name := StreamName(c.ID)
		want[name] = true
		srcs := []string{c.RTSPMainURL}
		if c.RTSPSubURL != "" {
			srcs = append(srcs, c.RTSPSubURL)
		}
		// Only (re)register when the sources actually changed. Re-PUTting an
		// unchanged stream would drop and recreate the producer, blipping every
		// active live viewer — so edits to name/mode/retention must not churn it.
		if cur, ok := existing[name]; ok && sameSources(cur, srcs) {
			continue
		}
		if err := m.putStream(ctx, name, srcs); err != nil {
			return fmt.Errorf("register stream %s: %w", name, err)
		}
	}
	// Remove go2rtc streams we manage (cam_*) that are no longer wanted.
	for name := range existing {
		if len(name) > 4 && name[:4] == "cam_" && !want[name] {
			_ = m.deleteStream(ctx, name)
		}
	}
	return nil
}

// putStream registers (or replaces) a stream's sources in go2rtc.
func (m *Manager) putStream(ctx context.Context, name string, sources []string) error {
	// Replace any existing definition first so source edits take effect.
	_ = m.deleteStream(ctx, name)
	for _, src := range sources {
		q := url.Values{"name": {name}, "src": {src}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, m.apiBase+"/api/streams?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		resp, err := m.http.Do(req)
		if err != nil {
			return err
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("go2rtc PUT streams: %s", resp.Status)
		}
	}
	return nil
}

func (m *Manager) deleteStream(ctx context.Context, name string) error {
	q := url.Values{"src": {name}}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, m.apiBase+"/api/streams?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return nil
}

// streams returns the set of currently-defined go2rtc stream names.
func (m *Manager) streams(ctx context.Context) (map[string]json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.apiBase+"/api/streams", nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("go2rtc GET streams: %s", resp.Status)
	}
	out := map[string]json.RawMessage{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// sameSources reports whether a go2rtc stream definition already has exactly the
// desired source URLs (order-independent), so we can skip a needless re-register.
func sameSources(raw json.RawMessage, srcs []string) bool {
	var def struct {
		Producers []struct {
			URL string `json:"url"`
		} `json:"producers"`
	}
	if err := json.Unmarshal(raw, &def); err != nil {
		return false
	}
	if len(def.Producers) != len(srcs) {
		return false
	}
	have := make(map[string]int, len(def.Producers))
	for _, p := range def.Producers {
		have[p.URL]++
	}
	for _, s := range srcs {
		if have[s] == 0 {
			return false
		}
		have[s]--
	}
	return true
}

// Online reports whether go2rtc currently has a producer for the camera (a
// best-effort health signal for the UI).
func (m *Manager) Online(ctx context.Context, cameraID string) bool {
	streams, err := m.streams(ctx)
	if err != nil {
		return false
	}
	_, ok := streams[StreamName(cameraID)]
	return ok
}

// ProxyWS rewrites an incoming request to go2rtc's live WebSocket for the named
// stream and proxies it (WebRTC signaling + MSE fallback flow through here).
func (m *Manager) ProxyWS(w http.ResponseWriter, r *http.Request, streamName string) {
	r.URL.Path = "/api/ws"
	r.URL.RawQuery = "src=" + url.QueryEscape(streamName)
	r.Host = m.proxyHost
	// go2rtc's WebSocket upgrader rejects cross-origin requests. We are a
	// trusted, authenticated reverse proxy in front of go2rtc, so strip the
	// browser Origin and present the request as same-origin to go2rtc.
	r.Header.Del("Origin")
	m.proxy.ServeHTTP(w, r)
}
