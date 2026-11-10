// Package detect adds opt-in object detection. Motion is still the cheap
// always-on trigger. Two ways to recognise objects:
//
//   - local: Aperture's own recognition (internal/ai). While a camera moves, its
//     frames run through a downloaded model a few times a second; objects are
//     drawn live and attached to the event (see live.go).
//   - bridge: when an event opens, a burst of snapshots goes to an external
//     inference server (CodeProject.AI, DeepStack, or a generic endpoint).
//
// With the mode "off" (the default) nothing runs and nothing is downloaded.
package detect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/aperture/internal/ai"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Mode values for Config.Mode.
const (
	ModeOff    = "off"
	ModeBridge = "bridge" // POST frames to an external inference server
	ModeLocal  = "local"  // Aperture's own recognition (internal/ai)
	ModeONNX   = "onnx"   // older name for local; read as local
)

// normMode maps the older "onnx" onto "local".
func normMode(m string) string {
	if m == ModeONNX {
		return ModeLocal
	}
	return m
}

// settingKey is where the detection config lives in the settings k/v table.
const settingKey = "detection"

// Detection is one object found in a frame; bbox is normalized 0..1.
type Detection struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	W          float64 `json:"w"`
	H          float64 `json:"h"`
	// Passes says whether local recognition would report it (the Try button
	// shows the rest as "also seen").
	Passes bool `json:"passes,omitempty"`
}

// MaxBurst caps the frames sampled per event. detectBurst loops `Burst` times
// with a network round-trip each, so an unbounded value is a denial-of-service
// vector. Enforced at read time in LoadConfig so it holds no matter how the
// setting was written (API, config restore, or a direct DB edit), and reused by
// the API handler so the two limits can't drift.
const MaxBurst = 5

// Config is the operator-controlled detection setup (stored as JSON in settings).
type Config struct {
	Mode          string             `json:"mode"`          // off | bridge | local
	BridgeURL     string             `json:"bridgeUrl"`     // e.g. http://host:32168/v1/vision/detection
	BridgeAPI     string             `json:"bridgeApi"`     // deepstack | generic
	ModelPath     string             `json:"modelPath"`     // onnx model file (Phase 2)
	MinConfidence float64            `json:"minConfidence"` // global floor when no per-label threshold
	Labels        map[string]float64 `json:"labels"`        // per-label min confidence; non-empty ⇒ allowlist
	Burst         int                `json:"burst"`         // frames sampled per event (default 1)
	// Local recognition: where it runs and with which model ("auto" or empty
	// picks for the hardware), and how readily it reports objects.
	Device      string `json:"device,omitempty"`
	Model       string `json:"model,omitempty"`
	Sensitivity string `json:"sensitivity,omitempty"` // low | normal | high
	// Packages lists the cameras watching for packages (see packages.go).
	Packages []string `json:"packages,omitempty"`
}

// defaultLabels are recognised when no list is set: people, vehicles and
// animals. Furniture, plants and the like make poor alarms.
var defaultLabels = []string{"person", "car", "truck", "bus", "motorcycle", "bicycle", "dog", "cat", "bird", "horse", "bear", "cow", "sheep"}

// threshold is the default minimum score for a label at a sensitivity. People
// matter most and false ones annoy most, so they need a confident score.
func threshold(label, sensitivity string) float64 {
	base := 0.6
	switch label {
	case "person":
		base = 0.65
	case "bird", "cat":
		base = 0.55
	}
	switch sensitivity {
	case "low":
		base += 0.1
	case "high":
		base -= 0.15
	}
	return base
}

// liveAllow decides whether local recognition reports an object.
func (c Config) liveAllow(label string, score float64) bool {
	if len(c.Labels) > 0 {
		th, ok := c.Labels[label]
		if !ok {
			return false
		}
		if th <= 0 {
			th = threshold(label, c.Sensitivity)
		}
		return score >= th
	}
	for _, l := range defaultLabels {
		if l == label {
			return score >= threshold(label, c.Sensitivity)
		}
	}
	return false
}

// aiSettings is the device and model choice for internal/ai.
func (c Config) aiSettings() ai.Settings {
	s := ai.Settings{Device: c.Device, Model: c.Model}
	if s.Device == "" {
		s.Device = "auto"
	}
	if s.Model == "" {
		s.Model = "auto"
	}
	return s
}

// allow reports whether a detection clears this config's thresholds. When Labels
// is non-empty it acts as an allowlist (only those labels, each at its own
// threshold); otherwise every label is kept at MinConfidence.
func (c Config) allow(label string, conf float64) bool {
	if th, ok := c.Labels[label]; ok {
		if th <= 0 { // listed without its own threshold
			th = c.MinConfidence
			if th <= 0 {
				th = 0.4
			}
		}
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
	SnapshotSub(ctx context.Context, cameraID string) ([]byte, error)
}

// Engine owns the active backend and enriches events with detections.
type Engine struct {
	st      *store.Store
	sm      snapshotter
	dataDir string
	log     *slog.Logger

	mu           sync.Mutex
	sig          string                         // signature of the config the backends were built for
	cfg          Config                         // last-loaded config
	backends     map[string]Backend             // built lazily per method ("bridge"); nil entry = unavailable
	onDetections func(eventID, cameraID string) // notified when objects are attached (change feed)

	ai              *ai.Manager // local recognition, when wired
	live            *Live
	pkgs            *packages
	onFramesChanged func() // asks the motion pipeline to re-check which frames it sends

	eventDir string             // where event pictures go (<dir>/<camera>/events)
	onEvent  func(*store.Event) // events this package raises (packages)
}

// SetEventSink says where pictures of the events detection raises itself (a
// package delivered or picked up) are kept, and who hears of them.
func (e *Engine) SetEventSink(dir string, fn func(*store.Event)) {
	e.eventDir, e.onEvent = dir, fn
}

// EnableLocal wires Aperture's own recognition in. publish sends change-feed
// messages; onFramesChanged is called when cameras start or stop needing
// colour frames. It returns the motion.FrameSink to give the motion pipeline.
func (e *Engine) EnableLocal(m *ai.Manager, publish func(typ, camera string, data any), onFramesChanged func()) *Live {
	e.ai = m
	e.live = newLive(e, m, publish)
	e.pkgs = newPackages(e, m)
	e.onFramesChanged = onFramesChanged
	return e.live
}

// Run keeps local recognition in step with the configuration and cameras.
func (e *Engine) Run(ctx context.Context) {
	if e.live == nil {
		<-ctx.Done()
		return
	}
	e.ApplyConfig()
	go e.live.run(ctx)
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.refreshCameras()
		}
	}
}

// ApplyConfig brings local recognition in line with the saved configuration:
// it starts or stops the detector (downloading nothing) and tells the motion
// pipeline which cameras need frames.
func (e *Engine) ApplyConfig() {
	if e.ai == nil {
		return
	}
	c := e.LoadConfig()
	if raw, err := e.st.GetSetting("hardware_profile"); err == nil && raw != "" {
		var p struct {
			CPUName string `json:"cpuName"`
		}
		if json.Unmarshal([]byte(raw), &p) == nil {
			e.ai.SetCPUName(p.CPUName)
		}
	}
	on := e.localCameras(c)
	pk := packageCameras(c, on)
	if len(on) > 0 || c.Mode == ModeLocal {
		s := c.aiSettings()
		s.Packages = len(pk) > 0
		e.ai.Configure(&s, false)
	} else {
		e.ai.Configure(nil, false)
	}
	e.live.setEnabled(on)
	e.pkgs.setCameras(pk)
	if e.onFramesChanged != nil {
		e.onFramesChanged()
	}
}

// EnableAndDownload turns local recognition on with these settings and
// downloads whatever they still need: the one-button setup.
func (e *Engine) EnableAndDownload(device, model string) error {
	c := e.LoadConfig()
	c.Mode = ModeLocal
	c.Device, c.Model = device, model
	if err := e.SaveConfig(c); err != nil {
		return err
	}
	on := e.localCameras(c)
	pk := packageCameras(c, on)
	s := c.aiSettings()
	s.Packages = len(pk) > 0
	e.ai.Configure(&s, true)
	e.live.setEnabled(on)
	e.pkgs.setCameras(pk)
	if e.onFramesChanged != nil {
		e.onFramesChanged()
	}
	return nil
}

// AI is the local recognition manager (nil when not wired).
func (e *Engine) AI() *ai.Manager { return e.ai }

func (e *Engine) refreshCameras() {
	if e.live == nil {
		return
	}
	c := e.LoadConfig()
	on := e.localCameras(c)
	e.live.mu.Lock()
	changed := len(on) != countEnabled(e.live.cams)
	for id := range on {
		if lc := e.live.cams[id]; lc == nil || !lc.enabled {
			changed = true
		}
	}
	e.live.mu.Unlock()
	if changed {
		e.ApplyConfig()
	}
}

func countEnabled(m map[string]*liveCam) int {
	n := 0
	for _, c := range m {
		if c.enabled {
			n++
		}
	}
	return n
}

// packageCameras are the cameras watching for packages that also use local
// recognition (it supplies the frames).
func packageCameras(c Config, local map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, id := range c.Packages {
		if local[id] {
			out[id] = true
		}
	}
	return out
}

// localCameras lists cameras using local recognition: their own setting, or the
// global mode when they have none. It rides on motion detection: a recording
// camera needs it on; a live-only camera (recording off) gets it for the live
// view without creating events.
func (e *Engine) localCameras(c Config) map[string]bool {
	out := map[string]bool{}
	cams, err := e.st.ListCameras()
	if err != nil {
		return out
	}
	for _, cam := range cams {
		mode := normMode(cam.DetectMethod)
		if mode == "" {
			mode = c.Mode
		}
		motionOn := cam.MotionDetection || cam.RecordMode == store.ModeMotion || cam.RecordMode == store.ModeNever
		if mode == ModeLocal && cam.Enabled && motionOn {
			out[cam.ID] = true
		}
	}
	return out
}

// Watch counts a live viewer of a camera (recognition then also runs while
// nothing moves); call the returned func when they leave.
func (e *Engine) Watch(cameraID string) func() {
	if e == nil || e.live == nil {
		return func() {}
	}
	return e.live.watch(cameraID)
}

// watchInterval is the gap between frames of a watched camera that isn't
// moving: enough to box someone standing still, cheap on a CPU.
func (e *Engine) watchInterval() time.Duration {
	if e.ai != nil && e.ai.Provider() == "cuda" {
		return 200 * time.Millisecond
	}
	return 700 * time.Millisecond
}

// liveInterval is the shortest gap between two frames of one camera.
func (e *Engine) liveInterval() time.Duration {
	if e.ai != nil && e.ai.Provider() == "cuda" {
		return 100 * time.Millisecond
	}
	return 300 * time.Millisecond
}

// savePath stores where an event's objects went (for playback boxes).
func (e *Engine) savePath(cameraID string, pts []store.TrackPoint) {
	if err := e.st.AddTrackPoints(cameraID, pts); err != nil {
		e.log.Warn("detect: save object paths", "err", err)
	}
}

// saveObjects stores an event's objects and announces them.
func (e *Engine) saveObjects(eventID, cameraID string, objs []store.EventObject) {
	if len(objs) == 0 {
		return
	}
	labels := map[string]bool{}
	for _, o := range objs {
		labels[o.Label] = true
	}
	if err := e.st.SaveEventDetections(eventID, objs[0].Label, summarize(objs), objs); err != nil {
		e.log.Error("detect: save detections", "err", err)
		return
	}
	if e.onDetections != nil {
		go e.onDetections(eventID, cameraID)
	}
}

// SetOnDetections registers a callback invoked (async) after detection objects
// are saved for an event. Used to emit the change feed's detection.created.
func (e *Engine) SetOnDetections(fn func(eventID, cameraID string)) { e.onDetections = fn }

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
	c.Mode = normMode(c.Mode)
	if c.Mode == "" {
		c.Mode = ModeOff
	}
	if c.Burst <= 0 {
		c.Burst = 1
	}
	if c.Burst > MaxBurst {
		c.Burst = MaxBurst
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

// backendFor returns (building lazily) the backend for a given method, using the
// global config c for connection details (bridge URL, model path). Returns nil
// for "off"/unavailable. All backends are torn down and rebuilt when the global
// config changes. Caller holds mu.
func (e *Engine) backendFor(mode string, c Config) Backend {
	sig := c.Mode + "|" + c.BridgeURL + "|" + c.BridgeAPI + "|" + c.ModelPath
	if sig != e.sig {
		for _, b := range e.backends {
			if b != nil {
				b.Close()
			}
		}
		e.backends = map[string]Backend{}
		e.sig = sig
		e.cfg = c
	}
	if mode == ModeOff || mode == "" {
		return nil
	}
	if b, ok := e.backends[mode]; ok {
		return b // cached (possibly nil for a known-broken backend)
	}
	var b Backend
	switch mode {
	case ModeBridge:
		b = newBridge(c)
	case ModeLocal:
		if e.ai != nil {
			b = &localBackend{m: e.ai}
		}
	}
	if e.backends == nil {
		e.backends = map[string]Backend{}
	}
	e.backends[mode] = b // cache nil too, so we don't retry a broken backend each event
	if b != nil {
		e.log.Info("detect: backend ready", "backend", b.Name())
	}
	return b
}

// effectiveMode is the object-detection method for a camera: its per-camera
// override when set, otherwise the global mode.
func (e *Engine) effectiveMode(cameraID, globalMode string) string {
	if cam, err := e.st.GetCamera(cameraID); err == nil && cam != nil && cam.DetectMethod != "" {
		return normMode(cam.DetectMethod)
	}
	if globalMode == "" {
		return ModeOff
	}
	return globalMode
}

// Enrich runs detection on a freshly-opened event and persists any objects
// found. It is a no-op when detection is off or the backend is unavailable, and
// it mutates ev in place so the notify stage can filter on the labels.
func (e *Engine) Enrich(ev *store.Event) {
	c := e.LoadConfig()
	// A camera may override the global method (including forcing it on when the
	// global default is off), so resolve the effective method per camera.
	mode := e.effectiveMode(ev.CameraID, c.Mode)
	if mode == ModeOff {
		return
	}
	if mode == ModeLocal {
		// Live recognition is already looking at this camera; give it a moment
		// so the event (and its notification) can say what moved.
		if e.live == nil {
			return
		}
		objs := e.live.waitFirst(ev.CameraID, ev.ID, 1500*time.Millisecond)
		if len(objs) == 0 {
			return
		}
		e.saveObjects(ev.ID, ev.CameraID, objs)
		labels := make([]string, 0, len(objs))
		for _, o := range objs {
			labels = append(labels, o.Label)
		}
		sort.Strings(labels)
		ev.TopLabel, ev.ObjectSummary, ev.Labels, ev.Objects = objs[0].Label, summarize(objs), labels, objs
		return
	}
	e.mu.Lock()
	backend := e.backendFor(mode, c)
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
	if e.onDetections != nil {
		go e.onDetections(ev.ID, ev.CameraID)
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
	// Test the method this camera would actually use (its override or the global).
	mode := e.effectiveMode(cameraID, c.Mode)
	e.mu.Lock()
	backend := e.backendFor(mode, c)
	e.mu.Unlock()
	if backend == nil {
		return mode, nil, errUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	var jpeg []byte
	var err error
	// Local recognition looks at the sub stream when there is one, so try
	// that picture; fall back to the main stream (and the other way round).
	cam, _ := e.st.GetCamera(cameraID)
	if mode == ModeLocal && cam != nil && cam.RTSPSubURL != "" {
		if jpeg, err = e.sm.SnapshotSub(ctx, cameraID); err != nil {
			jpeg, err = e.sm.Snapshot(ctx, cameraID)
		}
	} else if jpeg, err = e.sm.Snapshot(ctx, cameraID); err != nil && cam != nil && cam.RTSPSubURL != "" {
		jpeg, err = e.sm.SnapshotSub(ctx, cameraID)
	}
	if err != nil {
		return backend.Name(), nil, fmt.Errorf("couldn't get a picture from the camera: %w", err)
	}
	dets, err := backend.Detect(ctx, jpeg)
	if mode == ModeLocal {
		for i := range dets {
			dets[i].Passes = c.liveAllow(dets[i].Label, dets[i].Confidence)
		}
	}
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

// EventOpened and EventClosed tie motion events to live recognition.
func (e *Engine) EventOpened(ev *store.Event) {
	if e.live != nil {
		e.live.eventOpened(ev)
	}
}

// EventClosed saves the event's last objects.
func (e *Engine) EventClosed(eventID string) {
	if e.live != nil {
		e.live.eventClosed(eventID)
	}
}

// localBackend runs a snapshot through local recognition (the Test button).
type localBackend struct{ m *ai.Manager }

func (b *localBackend) Name() string { return "local" }
func (b *localBackend) Close()       {}

func (b *localBackend) Detect(ctx context.Context, jpeg []byte) ([]Detection, error) {
	img, _, err := image.Decode(bytes.NewReader(jpeg))
	if err != nil {
		return nil, err
	}
	// Scale to the model's size first: a 4K snapshot is 25 MB of RGB otherwise.
	size := b.m.InputSize()
	if size <= 0 {
		size = 640
	}
	rgb, w, h := toRGB(img, size)
	res, err := b.m.Detect(ctx, rgb, w, h)
	if err != nil {
		return nil, err
	}
	out := make([]Detection, 0, len(res.Boxes))
	for _, x := range res.Boxes {
		out = append(out, Detection{Label: ai.Label(x.Class), Confidence: round3(float64(x.Score)),
			X: round3(float64(x.X)), Y: round3(float64(x.Y)), W: round3(float64(x.W)), H: round3(float64(x.H))})
	}
	return out, nil
}

// toRGB converts an image to packed RGB, shrunk to fit (3·size)×size like the
// live frames, so a panoramic camera keeps enough detail to be tiled.
func toRGB(img image.Image, size int) ([]byte, int, int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	scale := min(1.0, float64(3*size)/float64(w), float64(size)/float64(h))
	nw, nh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	src := image.NewRGBA(b)
	draw.Draw(src, b, img, b.Min, draw.Src)
	out := make([]byte, nw*nh*3)
	for y := 0; y < nh; y++ {
		sy := min(h-1, int(float64(y)/scale))
		for x := 0; x < nw; x++ {
			sx := min(w-1, int(float64(x)/scale))
			p := src.PixOffset(b.Min.X+sx, b.Min.Y+sy)
			o := (y*nw + x) * 3
			out[o], out[o+1], out[o+2] = src.Pix[p], src.Pix[p+1], src.Pix[p+2]
		}
	}
	return out, nw, nh
}
