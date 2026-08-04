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
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

type cachedSnap struct {
	data []byte
	ts   time.Time
}

// Manager talks to a go2rtc instance over its HTTP API.
type Manager struct {
	apiBase   string // e.g. http://go2rtc:1984
	rtspBase  string // e.g. rtsp://go2rtc:8554
	http      *http.Client
	proxy     *httputil.ReverseProxy
	proxyHost string

	snapMu sync.RWMutex
	snaps  map[string]cachedSnap // cameraID -> last JPEG frame
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
		snaps:     map[string]cachedSnap{},
	}, nil
}

// StreamName is the go2rtc stream identifier for a camera's main (high) stream.
func StreamName(cameraID string) string { return "cam_" + cameraID }

// SubStreamName is the go2rtc identifier for a camera's low-quality sub stream,
// registered separately (when a sub URL is set) so the live view can request it.
func SubStreamName(cameraID string) string { return "cam_" + cameraID + "_sub" }

// RTSPURL is the internal restream URL recorders + Home Assistant consume.
func (m *Manager) RTSPURL(cameraID string) string {
	return fmt.Sprintf("%s/%s", m.rtspBase, StreamName(cameraID))
}

// RTSPURLSub is the internal restream URL for a camera's low-quality sub stream
// (registered only when the camera has a sub URL).
func (m *Manager) RTSPURLSub(cameraID string) string {
	return fmt.Sprintf("%s/%s", m.rtspBase, SubStreamName(cameraID))
}

// Sync reconciles go2rtc's streams with the given cameras: enabled cameras get
// their sources registered, others are removed.
// Sync reconciles go2rtc's stream set with the stored cameras and returns the
// number of streams it (re)registered. Because go2rtc holds dynamically-added
// streams only in memory, a go2rtc restart drops them all — so this must be run
// periodically, not just once, for live view to self-heal.
func (m *Manager) Sync(ctx context.Context, cameras []*store.Camera) (int, error) {
	existing, err := m.streams(ctx)
	if err != nil {
		return 0, err
	}
	want := map[string]bool{}
	registered := 0
	for _, c := range cameras {
		if !c.Enabled || c.RTSPMainURL == "" {
			continue
		}
		// Main (high) stream — used for recording, motion, and the default live
		// view. Only (re)register when the source actually changed: re-PUTting an
		// unchanged stream drops and recreates the producer, blipping every active
		// live viewer, so edits to name/mode/retention must not churn it.
		name := StreamName(c.ID)
		want[name] = true
		if cur, ok := existing[name]; !ok || !sameSources(cur, []string{c.RTSPMainURL}) {
			if err := m.putStream(ctx, name, []string{c.RTSPMainURL}); err != nil {
				return registered, fmt.Errorf("register stream %s: %w", name, err)
			}
			registered++
		}
		// Sub (low) stream — a separately addressable low-quality stream for the
		// live-view quality selector, registered only when a sub URL is set. It's
		// lazy in go2rtc (no producer until a client actually requests it).
		if c.RTSPSubURL != "" {
			sub := SubStreamName(c.ID)
			want[sub] = true
			if cur, ok := existing[sub]; !ok || !sameSources(cur, []string{c.RTSPSubURL}) {
				if err := m.putStream(ctx, sub, []string{c.RTSPSubURL}); err != nil {
					return registered, fmt.Errorf("register stream %s: %w", sub, err)
				}
				registered++
			}
		}
	}
	// Remove go2rtc streams we manage (cam_*) that are no longer wanted.
	for name := range existing {
		if len(name) > 4 && name[:4] == "cam_" && !want[name] {
			_ = m.deleteStream(ctx, name)
		}
	}
	return registered, nil
}

// Restart force re-registers a camera's go2rtc stream (delete + re-add) even if
// the sources are unchanged, tearing down a wedged producer so the recorder and
// live viewers reconnect to a fresh one. Used by the watchdog for recovery.
func (m *Manager) Restart(ctx context.Context, cam *store.Camera) error {
	if !cam.Enabled || cam.RTSPMainURL == "" {
		return nil
	}
	// Recover the main (recording) stream; the sub is live-view-only and the next
	// periodic Sync re-registers it if needed.
	return m.putStream(ctx, StreamName(cam.ID), []string{cam.RTSPMainURL})
}

// TestStream probes an arbitrary RTSP URL by registering it under a temporary
// go2rtc stream, fetching one frame, and tearing the stream down — so a camera's
// connection can be verified with a live preview before it is saved. The temp
// stream is always cleaned up.
func (m *Manager) TestStream(ctx context.Context, rtspURL string) ([]byte, error) {
	rtspURL = strings.TrimSpace(rtspURL)
	if !strings.HasPrefix(rtspURL, "rtsp://") {
		return nil, fmt.Errorf("must be an rtsp:// URL")
	}
	name := "test_" + store.NewID()
	if err := m.putStream(ctx, name, []string{rtspURL}); err != nil {
		return nil, fmt.Errorf("register test stream: %w", err)
	}
	// Always clean up, even if the context is already cancelled.
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.deleteStream(cctx, name)
	}()

	// Give go2rtc a moment to dial the producer before grabbing a frame.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(800 * time.Millisecond):
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.apiBase+"/api/frame.jpeg?src="+url.QueryEscape(name), nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the camera")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("no video from the camera (check URL/credentials)")
	}
	jpeg, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil || len(jpeg) == 0 {
		return nil, fmt.Errorf("no frame received")
	}
	return jpeg, nil
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

// StreamHealth is a snapshot of go2rtc's reachability and the camera streams it
// currently holds (name -> has at least one producer source).
type StreamHealth struct {
	Reachable  bool
	Registered map[string]bool
}

// Health probes go2rtc for the live-view health signal that a silent stream
// drop (e.g. after a go2rtc restart) would otherwise hide.
func (m *Manager) Health(ctx context.Context) StreamHealth {
	raw, err := m.streams(ctx)
	if err != nil {
		return StreamHealth{Reachable: false, Registered: map[string]bool{}}
	}
	h := StreamHealth{Reachable: true, Registered: make(map[string]bool, len(raw))}
	for name, def := range raw {
		var d struct {
			Producers []json.RawMessage `json:"producers"`
		}
		_ = json.Unmarshal(def, &d)
		h.Registered[name] = len(d.Producers) > 0
	}
	return h
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

// Snapshot fetches a single JPEG frame for a camera from go2rtc and caches it.
func (m *Manager) Snapshot(ctx context.Context, cameraID string) ([]byte, error) {
	u := m.apiBase + "/api/frame.jpeg?src=" + url.QueryEscape(StreamName(cameraID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("go2rtc frame.jpeg: %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	m.snapMu.Lock()
	m.snaps[cameraID] = cachedSnap{data: data, ts: time.Now()}
	m.snapMu.Unlock()
	return data, nil
}

// SnapshotCached returns a recent cached frame instantly when available, only
// hitting go2rtc when the cache is stale or empty. This makes the blurred
// loading placeholder appear immediately rather than waiting on go2rtc.
func (m *Manager) SnapshotCached(ctx context.Context, cameraID string) ([]byte, error) {
	m.snapMu.RLock()
	c, ok := m.snaps[cameraID]
	m.snapMu.RUnlock()
	if ok && time.Since(c.ts) < 20*time.Second {
		return c.data, nil
	}
	data, err := m.Snapshot(ctx, cameraID)
	if err != nil {
		if ok {
			return c.data, nil // serve stale rather than failing
		}
		return nil, err
	}
	return data, nil
}

// WarmSnapshots refreshes the cached frame for each enabled camera. Run it on a
// short interval so a fresh-ish frame is always ready to serve.
func (m *Manager) WarmSnapshots(ctx context.Context, cameras []*store.Camera) {
	for _, c := range cameras {
		if !c.Enabled {
			continue
		}
		_, _ = m.Snapshot(ctx, c.ID)
	}
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
