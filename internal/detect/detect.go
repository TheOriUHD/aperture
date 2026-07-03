// Package detect adds opt-in, event-gated object detection. Motion is still the
// cheap always-on trigger; when an event opens, the engine classifies a burst of
// frames and tags the event with the objects found (person, vehicle, animal…).
//
// The default build ships no detector and does nothing: the config mode is
// "off", Enrich is a no-op, and no model or inference runtime is required. A
// detector only ever runs on the handful of frames motion already flagged, so
// this never becomes a constant AI workload — honoring "No AI by default".
package detect

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// Mode values for Config.Mode.
const (
	ModeOff    = "off"
	ModeBridge = "bridge" // POST frames to an external inference server
	ModeONNX   = "onnx"   // embedded local model (Phase 2)
)

// settingKey is where the detection config lives in the settings k/v table.
const settingKey = "detection"

// Detection is one object found in a frame; bbox is normalized 0..1.
type Detection struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	X, Y, W, H float64
}

// Config is the operator-controlled detection setup (stored as JSON in settings).
type Config struct {
	Mode          string             `json:"mode"`          // off | bridge | onnx
	BridgeURL     string             `json:"bridgeUrl"`     // e.g. http://host:32168/v1/vision/detection
	BridgeAPI     string             `json:"bridgeApi"`     // deepstack | generic
	ModelPath     string             `json:"modelPath"`     // onnx model file (Phase 2)
	MinConfidence float64            `json:"minConfidence"` // global floor when no per-label threshold
	Labels        map[string]float64 `json:"labels"`        // per-label min confidence; non-empty ⇒ allowlist
	Burst         int                `json:"burst"`         // frames sampled per event (default 1)
}

// allow reports whether a detection clears this config's thresholds. When Labels
// is non-empty it acts as an allowlist (only those labels, each at its own
// threshold); otherwise every label is kept at MinConfidence.
func (c Config) allow(label string, conf float64) bool {
	if th, ok := c.Labels[label]; ok {
		return conf >= th
	}
	if len(c.Labels) > 0 {
		return false
	}
	min := c.MinConfidence
	if min <= 0 {
		min = 0.4
	}
	return conf >= min
}

// Backend is one inference implementation (external bridge or embedded model).
type Backend interface {
	Detect(ctx context.Context, jpeg []byte) ([]Detection, error)
	Name() string
	Close()
}

type snapshotter interface {
	Snapshot(ctx context.Context, cameraID string) ([]byte, error)
}

// Engine owns the active backend and enriches events with detections.
type Engine struct {
	st      *store.Store
	sm      snapshotter
	dataDir string
	log     *slog.Logger

	mu      sync.Mutex
	sig     string  // signature of the config the backend was built for
	cfg     Config  // last-loaded config
	backend Backend // nil when mode=off or backend unavailable
}

// New builds the engine. It does not touch any model until detection is enabled.
func New(st *store.Store, sm snapshotter, dataDir string, log *slog.Logger) *Engine {
	return &Engine{st: st, sm: sm, dataDir: dataDir, log: log}
}

// LoadConfig reads the current detection config from settings.
func (e *Engine) LoadConfig() Config {
	var c Config
	if raw, err := e.st.GetSetting(settingKey); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &c)
	}
	if c.Mode == "" {
		c.Mode = ModeOff
	}
	if c.Burst <= 0 {
		c.Burst = 1
	}
	return c
}

// SaveConfig persists the detection config to settings.
func (e *Engine) SaveConfig(c Config) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return e.st.SetSetting(settingKey, string(raw))
}

// ensureBackend (re)builds the backend when the config changed. Caller holds mu.
func (e *Engine) ensureBackend(c Config) Backend {
	sig := c.Mode + "|" + c.BridgeURL + "|" + c.BridgeAPI + "|" + c.ModelPath
	if sig == e.sig && (e.backend != nil || c.Mode == ModeOff) {
		return e.backend
	}
	if e.backend != nil {
		e.backend.Close()
		e.backend = nil
	}
	e.sig = sig
	e.cfg = c
	switch c.Mode {
	case ModeBridge:
		e.backend = newBridge(c)
	case ModeONNX:
		b, err := newONNX(c, e.dataDir, e.log)
		if err != nil {
			e.log.Warn("detect: onnx backend unavailable, detection disabled", "err", err)
			e.backend = nil
		} else {
			e.backend = b
		}
	default:
		e.backend = nil
	}
	if e.backend != nil {
		e.log.Info("detect: backend ready", "backend", e.backend.Name())
	}
	return e.backend
}

// Enrich runs detection on a freshly-opened event and persists any objects
// found. It is a no-op when detection is off or the backend is unavailable, and
// it mutates ev in place so the notify stage can filter on the labels.
func (e *Engine) Enrich(ev *store.Event) {
	c := e.LoadConfig()
	if c.Mode == ModeOff {
		return
	}
	e.mu.Lock()
	backend := e.ensureBackend(c)
	e.mu.Unlock()
	if backend == nil {
		return
	}

	dets := e.detectBurst(ev.CameraID, c, backend)
	if len(dets) == 0 {
		return
	}

	// Keep the best detections, build the summary, and persist.
	objs := make([]store.EventObject, 0, len(dets))
	labelSet := map[string]bool{}
	for _, d := range dets {
		objs = append(objs, store.EventObject{
			Label: d.Label, Confidence: d.Confidence,
			X: d.X, Y: d.Y, W: d.W, H: d.H, TS: ev.StartTS,
		})
		labelSet[d.Label] = true
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Confidence > objs[j].Confidence })

	top := objs[0].Label
	summary := summarize(objs)
	labels := make([]string, 0, len(labelSet))
	for l := range labelSet {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	if err := e.st.SaveEventDetections(ev.ID, top, summary, objs); err != nil {
		e.log.Error("detect: save detections", "err", err)
		return
	}
	ev.TopLabel, ev.ObjectSummary, ev.Labels, ev.Objects = top, summary, labels, objs
	e.log.Info("detect: tagged event", "event", ev.ID, "camera", ev.CameraID, "objects", summary)
}

// detectBurst samples up to Burst frames, runs the backend on each, and keeps the
// highest-confidence detection per label above the configured thresholds.
func (e *Engine) detectBurst(cameraID string, c Config, backend Backend) []Detection {
	best := map[string]Detection{}
	for i := 0; i < c.Burst; i++ {
		if i > 0 {
			time.Sleep(250 * time.Millisecond)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		jpeg, err := e.sm.Snapshot(ctx, cameraID)
		if err != nil || len(jpeg) == 0 {
			cancel()
			continue
		}
		dets, err := backend.Detect(ctx, jpeg)
		cancel()
		if err != nil {
			e.log.Warn("detect: backend error", "err", err)
			continue
		}
		for _, d := range dets {
			if !c.allow(d.Label, d.Confidence) {
				continue
			}
			if cur, ok := best[d.Label]; !ok || d.Confidence > cur.Confidence {
				best[d.Label] = d
			}
		}
	}
	out := make([]Detection, 0, len(best))
	for _, d := range best {
		out = append(out, d)
	}
	return out
}

// Test runs the current backend on a single fresh snapshot from a camera and
// returns the raw detections (used by the Settings "Test" button). Unlike
// Enrich it ignores thresholds so the operator can see everything the model
// reports while tuning.
func (e *Engine) Test(cameraID string) (string, []Detection, error) {
	c := e.LoadConfig()
	e.mu.Lock()
	backend := e.ensureBackend(c)
	e.mu.Unlock()
	if backend == nil {
		return c.Mode, nil, errUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	jpeg, err := e.sm.Snapshot(ctx, cameraID)
	if err != nil {
		return backend.Name(), nil, err
	}
	dets, err := backend.Detect(ctx, jpeg)
	return backend.Name(), dets, err
}

// summarize renders "person, car" (unique labels, most-confident first, capped).
func summarize(objs []store.EventObject) string {
	seen := map[string]bool{}
	var parts []string
	for _, o := range objs {
		if seen[o.Label] {
			continue
		}
		seen[o.Label] = true
		parts = append(parts, o.Label)
		if len(parts) >= 4 {
			break
		}
	}
	return strings.Join(parts, ", ")
}
