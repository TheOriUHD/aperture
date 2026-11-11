// Package watchdog supervises camera health: it detects a camera whose recording
// has gone stale (a wedged producer that hasn't exited, so the recorder's own
// backoff never fires) and recovers it by force re-registering the go2rtc stream
// and restarting the recorder on it. It also tracks last-seen and daily restart
// counts to surface in the health dashboard, and whether each camera is online:
// actually delivering video, not merely known to go2rtc.
package watchdog

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
)

const (
	interval       = 15 * time.Second
	recoverBackoff = 60 * time.Second // at most one recovery per camera per minute

	offlineAfter = 30 * time.Second // read, with nothing arriving for this long: offline
	probeEvery   = time.Minute      // how often a camera nothing reads is asked for
	probeTimeout = 10 * time.Second
	probeFails   = 2 // failed asks in a row before such a camera counts as offline
)

// Recorder is the part of the recorder a recovery goes through.
type Recorder interface {
	RecordingDue(c *store.Camera) bool
	NeedsRecovery(c *store.Camera) bool
	RestartCamera(id string)
}

type camState struct {
	lastSeen      int64
	restartsToday int
	stale         bool
	lastRecover   time.Time
}

// liveState is what the watchdog knows about whether a camera delivers.
type liveState struct {
	name         string
	known        bool // online has been worked out (until then it counts as online)
	online       bool
	recv         int64     // bytes go2rtc had received from it at the last tick
	reading      bool      // something reads it: recording, motion/recognition, a viewer
	readingSince time.Time // when that started
	lastMedia    time.Time // when bytes last arrived (zero: not seen yet)
	probedAt     time.Time
	fails        int // asks in a row that failed
}

// Watchdog periodically checks recording liveness and self-heals.
type Watchdog struct {
	st  *store.Store
	sm  *stream.Manager
	rec Recorder
	log *slog.Logger

	mu            sync.Mutex
	state         map[string]*camState
	live          map[string]*liveState
	dayStamp      string
	onCameraState func(cameraID string, online bool) // change-feed camera.state hook
	watching      func(cameraID string) bool         // motion/recognition reads the camera
	probe         func(ctx context.Context, cameraID string) error
	now           func() time.Time
}

// New constructs a Watchdog.
func New(st *store.Store, sm *stream.Manager, rec Recorder, log *slog.Logger) *Watchdog {
	w := &Watchdog{st: st, sm: sm, rec: rec, log: log,
		state: map[string]*camState{}, live: map[string]*liveState{}, now: time.Now}
	if sm != nil {
		w.probe = sm.Probe
	}
	return w
}

// SetOnCameraState registers a callback invoked (async) with a camera's
// online/offline state once it is known and on every transition. Used to emit
// the change feed's camera.state (and seed its snapshot).
func (w *Watchdog) SetOnCameraState(fn func(cameraID string, online bool)) { w.onCameraState = fn }

// SetWatching tells the watchdog which cameras motion detection or object
// recognition reads, so a camera whose reader gets nothing counts as offline.
func (w *Watchdog) SetWatching(fn func(cameraID string) bool) { w.watching = fn }

// Run checks every interval until ctx is cancelled.
func (w *Watchdog) Run(ctx context.Context) {
	t := time.NewTicker(interval)
	defer t.Stop()
	w.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *Watchdog) tick(ctx context.Context) {
	cams, err := w.st.ListCameras()
	if err != nil {
		return
	}
	now := w.now()
	day := now.UTC().Format("2006-01-02")

	w.mu.Lock()
	if day != w.dayStamp {
		w.dayStamp = day
		for _, s := range w.state {
			s.restartsToday = 0
		}
	}
	w.mu.Unlock()

	probe := w.observe(cams, w.sm.Health(ctx), now)

	nowMs := now.UnixMilli()
	for _, c := range cams {
		// Only cameras that are supposed to be recording can be "stale".
		if !c.Enabled || c.RecordMode == store.ModeNever {
			continue
		}
		var newest int64
		if stat, err := w.st.CameraStat(c.ID); err == nil {
			newest = stat.NewestTS
		}
		// newest==0 means the camera has never produced a segment — don't flag a
		// brand-new/never-working camera as "stale" (recordingOk already shows it).
		// Neither does a camera outside its schedule: its footage is old on purpose.
		stale := newest > 0 && nowMs-newest > store.RecordingGraceMs && w.rec.RecordingDue(c)
		// Stale says footage isn't arriving, which a failing upload causes too. Only
		// a recorder that has stopped writing is worth recovering: restarting the
		// stream under a healthy one is what the comment below warns about.
		stalled := stale && w.rec.NeedsRecovery(c)

		w.mu.Lock()
		s := w.state[c.ID]
		if s == nil {
			s = &camState{}
			w.state[c.ID] = s
		}
		s.lastSeen = newest
		s.stale = stale
		needRecover := stalled && time.Since(s.lastRecover) > recoverBackoff
		if needRecover {
			s.lastRecover = now
			s.restartsToday++
		}
		w.mu.Unlock()

		if needRecover {
			cam := c
			w.log.Warn("watchdog: recording stale, restarting stream",
				"camera", c.ID, "name", c.Name, "lastSegmentAgeSec", (nowMs-newest)/1000)
			if err := w.sm.Restart(ctx, cam); err != nil {
				w.log.Warn("watchdog: recovery restart failed", "camera", c.ID, "err", err)
			}
			// ffmpeg stays connected through a stream restart and carries on from the
			// new source's timestamps, which can be hours away from the old ones: the
			// segmenter then cuts at every keyframe, or never. Start it fresh as well.
			w.rec.RestartCamera(c.ID)
		}
	}

	w.runProbes(ctx, probe)
}

// observe works out which cameras deliver. Whatever reads a camera (recording,
// motion and recognition, a viewer) reads it through go2rtc, so the bytes go2rtc
// has received from it growing between ticks means it delivers, and being read
// with none arriving for offlineAfter means it doesn't. A camera nothing reads
// has to be asked: observe returns those due a probe.
func (w *Watchdog) observe(cams []*store.Camera, h stream.StreamHealth, now time.Time) (probe []string) {
	type seen struct {
		c         *store.Camera
		recv      int64
		connected bool
		reading   bool
	}
	var list []seen
	for _, c := range cams {
		if !c.Enabled {
			continue
		}
		main, sub := stream.StreamName(c.ID), stream.SubStreamName(c.ID)
		s := seen{c: c}
		for _, n := range []string{main, sub} {
			if b, ok := h.Recv[n]; ok {
				s.recv += b
				s.connected = true
			}
		}
		// Asked outside the lock: these take the recorder's and detector's own.
		s.reading = h.Consumers[main]+h.Consumers[sub] > 0 || w.rec.RecordingDue(c) ||
			(w.watching != nil && w.watching(c.ID))
		list = append(list, s)
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	keep := map[string]bool{}
	for _, s := range list {
		id := s.c.ID
		keep[id] = true
		if !h.Reachable {
			continue // go2rtc is down: nothing to tell, and live view says so already
		}
		l := w.live[id]
		if l == nil {
			l = &liveState{}
			w.live[id] = l
		}
		l.name = s.c.Name
		media := s.connected && s.recv > 0 && s.recv != l.recv
		l.recv = s.recv
		if s.reading && !l.reading {
			l.readingSince = now
		}
		l.reading = s.reading
		switch {
		case media:
			l.lastMedia, l.fails = now, 0
			w.setOnline(id, l, true)
		case s.reading:
			// Read since readingSince, and nothing came for offlineAfter.
			from := l.readingSince
			if l.lastMedia.After(from) {
				from = l.lastMedia
			}
			if now.Sub(from) >= offlineAfter {
				w.setOnline(id, l, false)
			}
		case !l.lastMedia.IsZero() && now.Sub(l.lastMedia) < probeEvery:
			// Read until a moment ago, and it delivered.
		case now.Sub(l.probedAt) >= probeEvery:
			probe = append(probe, id)
		}
	}
	for id := range w.live {
		if !keep[id] {
			delete(w.live, id) // disabled or removed: starts afresh when back
		}
	}
	return probe
}

// runProbes asks go2rtc for each camera's stream, all at once.
func (w *Watchdog) runProbes(ctx context.Context, ids []string) {
	if len(ids) == 0 || w.probe == nil {
		return
	}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := w.probe(pctx, id)
			w.mu.Lock()
			defer w.mu.Unlock()
			l := w.live[id]
			if l == nil {
				return
			}
			l.probedAt = w.now()
			if err == nil {
				l.fails = 0
				w.setOnline(id, l, true)
				return
			}
			l.fails++
			if l.fails >= probeFails {
				w.log.Debug("watchdog: camera doesn't answer", "camera", id, "err", err)
				w.setOnline(id, l, false)
			} else {
				l.probedAt = time.Time{} // ask again on the next tick before calling it offline
			}
		}()
	}
	wg.Wait()
}

// setOnline records a camera's state and reports it when it is first known and
// on every change. Called with w.mu held.
func (w *Watchdog) setOnline(id string, l *liveState, online bool) {
	if l.known && l.online == online {
		return
	}
	if l.known {
		if online {
			w.log.Info("watchdog: camera online again", "camera", id, "name", l.name)
		} else {
			w.log.Warn("watchdog: camera offline", "camera", id, "name", l.name)
		}
	}
	l.known, l.online = true, online
	if w.onCameraState != nil {
		go w.onCameraState(id, online)
	}
}

// Online reports whether a camera delivers video, as far as the watchdog has
// seen; true until it knows otherwise.
func (w *Watchdog) Online(cameraID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	l := w.live[cameraID]
	return l == nil || !l.known || l.online
}

// Stats returns the watchdog's view of a camera for the health dashboard.
func (w *Watchdog) Stats(cameraID string) (lastSeen int64, restartsToday int, stale bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.state[cameraID]
	if s == nil {
		return 0, 0, false
	}
	return s.lastSeen, s.restartsToday, s.stale
}
