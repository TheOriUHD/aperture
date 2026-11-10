package ai

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Settings is what the operator chose: a device and a model, each "auto" by
// default.
type Settings struct {
	Device string `json:"device"` // auto | cpu | cuda
	Model  string `json:"model"`  // auto | a model id
	// Packages: some camera watches for parcels.
	Packages bool `json:"packages,omitempty"`
	// PackageMode is how parcels are found: "live" by an all-in-one main model
	// on every frame, "separate" by a small extra model every few seconds, or
	// "auto" (live when the main model can, else separate).
	PackageMode string `json:"packageMode,omitempty"`
}

// ModelView is a model as the settings page shows it.
type ModelView struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Detail      string  `json:"detail"`
	Input       int     `json:"input"`
	Size        int64   `json:"size"`
	Installed   bool    `json:"installed"`
	Recommended bool    `json:"recommended"`
	Ms          float64 `json:"ms"` // rough time per frame on the chosen device
	Arch        string  `json:"arch"`
	Labels      string  `json:"labels"`  // coco80 | coco91 | obj365 | custom
	Classes     int     `json:"classes"` // how many classes it knows
	AllInOne    bool    `json:"allInOne,omitempty"`
	GPUOnly     bool    `json:"gpuOnly,omitempty"`
	Tier        string  `json:"tier,omitempty"`
	License     string  `json:"license,omitempty"`
	Custom      bool    `json:"custom,omitempty"`
}

func viewOf(mm *Model, d Device, installed, recommended bool) ModelView {
	v := ModelView{ID: mm.ID, Name: mm.Name, Detail: mm.Detail, Input: mm.Input, Size: mm.Artifact.Size,
		Installed: installed, Recommended: recommended, Ms: mm.MsCPU, Arch: mm.ArchOf(), Labels: mm.LabelSetOf(),
		Classes: len(mm.ClassList()), AllInOne: mm.AllInOne, GPUOnly: mm.GPUOnly, Tier: mm.Tier, License: mm.License,
		Custom: mm.Custom}
	if d.Kind == "gpu" {
		v.Ms = mm.MsGPU
	}
	return v
}

// Status is the state of object recognition for the settings page.
type Status struct {
	// State is one of: off, needs-download, installing, starting, running, error.
	State    string      `json:"state"`
	Devices  []Device    `json:"devices"`
	Device   Device      `json:"device"`
	Model    ModelView   `json:"model"`
	Models   []ModelView `json:"models"`
	Settings Settings    `json:"settings"`
	Download int64       `json:"download"` // bytes still to fetch for this setup
	DiskFree int64       `json:"diskFree"`
	Install  Progress    `json:"install"`
	Runtime  string      `json:"runtime,omitempty"`
	Provider string      `json:"provider,omitempty"` // what actually runs: cpu | cuda
	Ms       float64     `json:"ms,omitempty"`       // average time per frame
	Rate     float64     `json:"rate,omitempty"`     // frames per second, last 10 s
	Error    string      `json:"error,omitempty"`
	Note     string      `json:"note,omitempty"` // e.g. why the GPU isn't used
	Files    int64       `json:"files"`          // bytes of installed downloads
	// Upgrade is a faster device "automatic" would now pick (a GPU added after
	// setup) whose files aren't downloaded yet; recognition keeps running on
	// the CPU meanwhile. UpgradeDownload is what switching would fetch.
	Upgrade         *Device `json:"upgrade,omitempty"`
	UpgradeDownload int64   `json:"upgradeDownload,omitempty"`
	// Packages is the separate package model for this device; PackagesRunning
	// says parcels are being looked for (by it, or live by the main model:
	// PackagesLive).
	Packages        *ModelView `json:"packages,omitempty"`
	PackagesRunning bool       `json:"packagesRunning,omitempty"`
	PackagesLive    bool       `json:"packagesLive,omitempty"`
}

// Manager installs runtimes and models on request and keeps a detector
// worker running for the chosen setup.
type Manager struct {
	store    Store
	bin      string
	threads  int
	log      *slog.Logger
	onChange func()
	onWorker func()

	mu         sync.Mutex
	want       *Settings // nil: object recognition is off
	gen        int       // bumped on every reconfiguration
	installing bool
	installGen int           // gen of the running download
	installed  chan struct{} // closed once the latest download has stopped
	runGen     int           // gen of the loop keeping the detector running (0: none)
	progress   Progress
	cancel     context.CancelFunc
	worker     *Worker
	device     Device
	model      *Model
	pkgModel   *Model // separate package model the worker has loaded
	state      string
	err, note  string
	ms         float64
	recent     []time.Time
	devCache   []Device
	devAt      time.Time
	cpuName    string  // from the hardware profile, when set
	upgrade    *Device // see Status.Upgrade
}

// SetCPUName names the CPU as the operator's hardware profile does: inside a
// VM the processor only reports itself as a virtual CPU.
func (m *Manager) SetCPUName(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name != m.cpuName {
		m.cpuName = name
		m.devCache = nil
	}
}

// NewManager makes a manager keeping its files in dir and running the worker
// binary bin with up to threads CPU threads.
func NewManager(dir, bin string, threads int, log *slog.Logger) *Manager {
	return &Manager{store: Store{Dir: dir}, bin: bin, threads: max(1, threads), log: log, state: "off"}
}

// SetOnWorker registers a callback for when a detector starts or stops (the
// device it runs on may change how many frames are worth sending). It is
// called without the manager's lock held.
func (m *Manager) SetOnWorker(fn func()) { m.onWorker = fn }

func (m *Manager) workerChanged() {
	if m.onWorker != nil {
		go m.onWorker()
	}
}

// SetOnChange registers a callback for status changes (install progress,
// worker start/stop); it may be called often during a download.
func (m *Manager) SetOnChange(fn func()) { m.onChange = fn }

func (m *Manager) changed() {
	if m.onChange != nil {
		m.onChange()
	}
}

func (m *Manager) devices() []Device {
	if m.devCache == nil || time.Since(m.devAt) > 30*time.Second {
		m.devCache = Devices(m.threads)
		m.devAt = time.Now()
		for i := range m.devCache {
			if m.devCache[i].ID == "cpu" && m.cpuName != "" {
				m.devCache[i].Name = m.cpuName
			}
		}
	}
	return m.devCache
}

// resolve turns settings into a device, a pack and a model.
func (m *Manager) resolve(s Settings) (Device, *Pack, *Model) {
	d := PickDevice(m.devices(), s.Device)
	p := PackByID(d.PackID)
	mod := m.modelByID(s.Model)
	if mod == nil {
		mod = ModelByID(DefaultModel(d))
	}
	return d, p, mod
}

// modelByID finds a catalog model or one the operator added.
func (m *Manager) modelByID(id string) *Model {
	if mod := ModelByID(id); mod != nil {
		return mod
	}
	return m.store.CustomModel(id)
}

// HasModel reports whether a model id exists (catalog or added).
func (m *Manager) HasModel(id string) bool { return m.modelByID(id) != nil }

// packageModel is the separate package model the settings need on a device:
// none when no camera watches for packages, or when the main model finds
// parcels itself and the separate check wasn't asked for.
func packageModel(s Settings, d Device, mod *Model) *Model {
	if !s.Packages {
		return nil
	}
	if s.PackageMode != "separate" && mod != nil && mod.AllInOne {
		return nil
	}
	return PackageModelFor(d)
}

func sameModel(a, b *Model) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ID == b.ID
}

// Plan reports what enabling with these settings would download.
func (m *Manager) Plan(s Settings) (Device, *Model, int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, p, mod := m.resolve(s)
	return d, mod, Bytes(m.store.Missing(p, mod, packageModel(s, d, mod)))
}

// Configure applies the operator's choice. nil turns recognition off (files
// stay). With download set, missing files are fetched; otherwise the status
// asks for the download.
func (m *Manager) Configure(s *Settings, download bool) {
	m.mu.Lock()
	if s != nil && !download && m.installing && m.want != nil && *m.want == *s {
		// Nothing changed (settings are re-applied whenever a camera is
		// saved): the running download applies this setup when it's done.
		m.mu.Unlock()
		return
	}
	m.gen++
	gen := m.gen
	m.want = s
	if m.cancel != nil && (s == nil || download) {
		m.cancel()
		m.cancel = nil
	}
	if s == nil {
		m.stopWorkerLocked()
		m.state, m.err, m.note = "off", "", ""
		m.mu.Unlock()
		m.changed()
		return
	}
	d, p, mod := m.resolve(*s)
	pm := packageModel(*s, d, mod)
	jobs := m.store.Missing(p, mod, pm)
	if len(jobs) > 0 && !download && m.store.updating(jobs) {
		// A newer version of something already installed (Aperture was
		// updated): fetch it without asking again; the setup was agreed to.
		m.log.Info("object recognition: updating downloaded files")
		download = true
	}
	// While downloading for the GPU that was offered, the offer stands (it's
	// shown again if the download is cancelled); otherwise it's worked out anew.
	if !download || m.upgrade == nil || m.upgrade.ID != d.ID {
		m.upgrade = nil
	}
	if !download && (s.Device == "" || s.Device == "auto") && d.Kind == "gpu" && !m.store.PackInstalled(p) {
		// A GPU appeared after setup. Keep recognising on the CPU with what is
		// already downloaded, and offer the switch instead of stopping.
		cpu := PickDevice(m.devices(), "cpu")
		cp := PackByID("cpu")
		cm := m.modelByID(s.Model)
		if cm == nil || cm.GPUOnly {
			cm = ModelByID(DefaultModel(cpu))
		}
		cpm := packageModel(*s, cpu, cm)
		if len(m.store.Missing(cp, cm)) == 0 {
			gpu := d
			m.upgrade = &gpu
			d, p, mod, pm = cpu, cp, cm, cpm
			jobs = m.store.Missing(p, mod, pm)
		}
	}
	if len(jobs) > 0 && !download && onlyModels(jobs) && m.store.PackInstalled(p) {
		// The runtime is there and only a model is missing: one just chosen,
		// the package model for a camera that started watching for parcels,
		// or a new automatic default. It comes without asking again, like an
		// update; recognition keeps running meanwhile.
		m.log.Info("object recognition: downloading a model")
		download = true
	}
	switch {
	case len(jobs) > 0 && download:
		if m.cancel != nil {
			// A download started on its own (a model just chosen) replaces
			// the running one; both would share tmp/.
			m.cancel()
		}
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		prev, done := m.installed, make(chan struct{})
		m.installed = done
		m.installing, m.installGen = true, gen
		m.progress = Progress{Active: true, Stage: "Starting the download", Phase: "download", Total: Bytes(jobs)}
		m.state, m.err, m.note = "installing", "", ""
		// A detector already running keeps recognising until the new files
		// are in place (the switch to a GPU is a 3 GB download).
		m.mu.Unlock()
		go m.install(ctx, gen, jobs, prev, done)
	case len(jobs) > 0:
		m.stopWorkerLocked()
		if !m.installing {
			m.state = "needs-download"
		}
		m.mu.Unlock()
	default:
		same := m.worker != nil && m.device.ID == d.ID && sameModel(m.model, mod) && sameModel(m.pkgModel, pm)
		if !same {
			m.stopWorkerLocked()
			m.state, m.err = "starting", ""
			m.runGen = gen
			m.mu.Unlock()
			go m.run(gen, d, p, mod, pm)
		} else {
			m.state, m.err = "running", ""
			m.mu.Unlock()
		}
	}
	m.changed()
}

func (m *Manager) install(ctx context.Context, gen int, jobs []job, prev, done chan struct{}) {
	if prev != nil {
		<-prev // the replaced download has stopped (it may clear tmp/ when done)
	}
	err := ctx.Err()
	if err == nil {
		err = m.store.Install(ctx, jobs, func(p Progress) {
			m.mu.Lock()
			if m.installGen == gen {
				m.progress = p
			}
			m.mu.Unlock()
			m.changed()
		})
	}
	close(done)
	m.mu.Lock()
	if m.installGen != gen {
		// A newer download replaced this one.
		m.mu.Unlock()
		return
	}
	m.installing = false
	m.progress = Progress{}
	m.cancel = nil
	if err != nil {
		running := m.worker != nil
		switch {
		case gen != m.gen && errors.Is(err, context.Canceled):
			// Turned off meanwhile: Configure already said so.
		case errors.Is(err, context.Canceled) && running:
			m.state = "running"
		case errors.Is(err, context.Canceled):
			m.state = "needs-download"
		case running:
			// Keep recognising with what works, and say why the switch failed.
			m.state, m.note = "running", "The download failed: "+err.Error()
			m.log.Warn("object recognition: download failed", "err", err)
		default:
			m.state, m.err = "error", err.Error()
			m.log.Warn("object recognition: download failed", "err", err)
		}
		m.mu.Unlock()
		m.changed()
		return
	}
	want := m.want
	// The running detector (if any) used the old files: start afresh.
	m.stopWorkerLocked()
	m.mu.Unlock()
	m.workerChanged()
	m.log.Info("object recognition: download complete")
	if want != nil {
		m.Configure(want, false)
	}
}

// run starts the worker and restarts it if it dies, until reconfigured.
func (m *Manager) run(gen int, d Device, p *Pack, mod *Model, pm *Model) {
	backoff := time.Second
	provider := "cpu"
	if d.Kind == "gpu" && d.ID == "cuda" {
		provider = "cuda"
	}
	for {
		cfg := WorkerConfig{
			Bin: m.bin, Lib: m.store.LibPath(p), Model: m.store.ModelPath(mod),
			Provider: provider, Threads: m.threads, LibDirs: m.store.LibDirs(p), MinScore: 0.25,
			Arch: mod.ArchOf(), Classes: m.store.ClassesArg(mod),
		}
		if mod.AllInOne {
			// Parcels score low (a flat box at night: 0.2-0.4); nvrd decides.
			cfg.MinScore = 0.12
		}
		if pm != nil {
			cfg.Packages = m.store.ModelPath(pm)
		}
		ctx, cancel := context.WithCancel(context.Background())
		w, err := StartWorker(ctx, cfg, m.log)
		cancel()
		m.mu.Lock()
		if gen != m.runGen {
			m.mu.Unlock()
			if w != nil {
				w.Close()
			}
			return
		}
		if err != nil && provider == "cuda" {
			// The GPU didn't work out: say why, and use the CPU meanwhile. The
			// GPU build of ONNX Runtime runs on the CPU as well.
			m.note = "Couldn't use the GPU (" + firstLine(err.Error()) + "). Running on the CPU instead."
			m.log.Warn("object recognition: GPU unavailable, using the CPU", "err", err)
			provider = "cpu"
			m.mu.Unlock()
			m.changed()
			continue
		}
		if err != nil {
			m.setRunState("error", firstLine(err.Error()))
			m.mu.Unlock()
			m.changed()
			m.log.Warn("object recognition: detector failed to start", "err", err)
		} else {
			m.worker, m.device, m.model, m.pkgModel = w, d, mod, pm
			m.setRunState("running", "")
			m.ms, m.recent = 0, nil
			h := w.Hello()
			m.mu.Unlock()
			m.changed()
			m.workerChanged()
			m.log.Info("object recognition running", "device", d.Name, "provider", h.Provider, "model", mod.ID, "ort", h.Version, "warmupMs", h.WarmupMs)
			<-w.Done()
			m.mu.Lock()
			if gen != m.runGen {
				m.mu.Unlock()
				return
			}
			m.worker = nil
			m.setRunState("starting", "The detector stopped and is restarting")
			m.mu.Unlock()
			m.changed()
			m.workerChanged()
			m.log.Warn("object recognition: detector exited", "err", w.Err())
		}
		time.Sleep(backoff)
		backoff = min(backoff*2, time.Minute)
		m.mu.Lock()
		stale := gen != m.runGen
		m.mu.Unlock()
		if stale {
			return
		}
	}
}

// setRunState records the detector's state, unless a download is under way:
// that shows, while the detector keeps running beside it.
func (m *Manager) setRunState(state, err string) {
	if !m.installing {
		m.state, m.err = state, err
	}
}

// stopWorkerLocked stops the detector and the loop keeping it running.
func (m *Manager) stopWorkerLocked() {
	m.runGen = 0
	if m.worker != nil {
		w := m.worker
		m.worker = nil
		go w.Close()
	}
}

// Cancel stops a running download.
func (m *Manager) Cancel() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.mu.Unlock()
}

// RemoveFiles deletes every download. Recognition must be off.
func (m *Manager) RemoveFiles() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.want != nil || m.installing {
		return errors.New("turn object recognition off first")
	}
	for _, sub := range []string{"packs", "models", "tmp"} {
		if err := os.RemoveAll(filepath.Join(m.store.Dir, sub)); err != nil {
			return err
		}
	}
	return nil
}

// Running reports whether a detector is ready for frames.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.worker != nil
}

// InputSize is the running model's input side (0 when nothing runs). Frames
// bigger than this are scaled down before they reach the worker anyway.
func (m *Manager) InputSize() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.worker == nil {
		if m.want != nil {
			_, _, mod := m.resolve(*m.want)
			return mod.Input
		}
		return 0
	}
	return m.worker.Hello().Input
}

// Detect runs one frame.
func (m *Manager) Detect(ctx context.Context, rgb []byte, w, h int) (Result, error) {
	m.mu.Lock()
	wk := m.worker
	m.mu.Unlock()
	if wk == nil {
		return Result{}, ErrWorkerGone
	}
	res, err := wk.Detect(ctx, rgb, w, h)
	if err == nil {
		m.mu.Lock()
		if m.ms == 0 {
			m.ms = res.Ms
		} else {
			m.ms = m.ms*0.9 + res.Ms*0.1
		}
		now := time.Now()
		m.recent = append(m.recent, now)
		cut := 0
		for cut < len(m.recent) && now.Sub(m.recent[cut]) > 10*time.Second {
			cut++
		}
		m.recent = m.recent[cut:]
		m.mu.Unlock()
	}
	return res, err
}

// PackagesReady reports whether parcels can be looked for: by the separate
// package model, or by the main model itself.
func (m *Manager) PackagesReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.worker != nil && (m.worker.Hello().Packages || m.liveLocked())
}

// PackagesLive reports whether the main model finds parcels on every frame
// (an all-in-one model, and the separate check not asked for).
func (m *Manager) PackagesLive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.liveLocked()
}

func (m *Manager) liveLocked() bool {
	return m.worker != nil && m.worker.Hello().Combined && m.pkgModel == nil &&
		m.want != nil && m.want.Packages && m.want.PackageMode != "separate"
}

// DetectPackages looks for people and packages in one frame: person boxes
// carry class 0, packages PackageClass. It uses the separate package model,
// or the main model when that finds parcels itself.
func (m *Manager) DetectPackages(ctx context.Context, rgb []byte, w, h int) (Result, error) {
	m.mu.Lock()
	wk := m.worker
	live := m.liveLocked()
	m.mu.Unlock()
	if wk == nil {
		return Result{}, ErrWorkerGone
	}
	if !wk.Hello().Packages {
		if !live {
			return Result{}, ErrWorkerGone
		}
		res, err := wk.DetectKind(ctx, KindObjects, rgb, w, h)
		if err == nil {
			res.Boxes = PeopleAndPackages(res.Boxes)
		}
		return res, err
	}
	return wk.DetectKind(ctx, KindPackages, rgb, w, h)
}

// PeopleAndPackages keeps the person and package boxes.
func PeopleAndPackages(boxes []Box) []Box {
	out := boxes[:0:0]
	for _, b := range boxes {
		if b.Class == 0 || b.Class == PackageClass {
			out = append(out, b)
		}
	}
	return out
}

// Status describes the current state.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{State: m.state, Error: m.err, Note: m.note, Install: m.progress, DiskFree: m.store.FreeBytes()}
	s.Devices = m.devices()
	want := Settings{Device: "auto", Model: "auto"}
	if m.want != nil {
		want = *m.want
	}
	s.Settings = want
	d, p, mod := m.resolve(want)
	if m.worker != nil {
		d, mod = m.device, m.model
		h := m.worker.Hello()
		s.Runtime = "ONNX Runtime " + h.Version
		s.Provider = h.Provider
		s.Ms = m.ms
		s.Rate = float64(len(m.recent)) / 10
	}
	s.Device = d
	def := DefaultModel(d)
	all := make([]*Model, 0, len(Models))
	for i := range Models {
		all = append(all, &Models[i])
	}
	for _, cm := range m.store.CustomModels() {
		all = append(all, cm)
	}
	for _, mm := range all {
		v := viewOf(mm, d, m.store.ModelInstalled(mm), mm.ID == def)
		s.Models = append(s.Models, v)
		if mod != nil && mm.ID == mod.ID {
			s.Model = v
		}
	}
	pm := packageModel(want, d, mod)
	// The separate package model is described whether or not a camera wants
	// it (the page shows what watching for packages would download).
	if v := PackageModelFor(d); v != nil {
		pv := viewOf(v, d, m.store.ModelInstalled(v), true)
		s.Packages = &pv
	}
	s.PackagesLive = m.liveLocked()
	s.PackagesRunning = m.worker != nil && (m.worker.Hello().Packages || s.PackagesLive)
	if !m.installing {
		s.Download = Bytes(m.store.Missing(p, mod, pm))
	}
	if m.upgrade != nil {
		u := *m.upgrade
		s.Upgrade = &u
		gm := m.modelByID(want.Model)
		if gm == nil {
			gm = ModelByID(DefaultModel(u))
		}
		s.UpgradeDownload = Bytes(m.store.Missing(PackByID(u.PackID), gm, packageModel(want, u, gm)))
		if m.worker == nil {
			s.Download = 0
		}
	}
	s.Files = dirSize(m.store.Dir)
	return s
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			n += info.Size()
		}
		return nil
	})
	return n
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' {
			return s[:i]
		}
	}
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// HumanBytes formats a size for messages.
func HumanBytes(n int64) string { return humanBytes(n) }

// Provider is what the running detector uses ("cpu" or "cuda"; "" when off).
func (m *Manager) Provider() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.worker == nil {
		return ""
	}
	return m.worker.Hello().Provider
}
