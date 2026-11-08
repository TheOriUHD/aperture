// Package watchdog supervises camera health: it detects a camera whose recording
// has gone stale (a wedged producer that hasn't exited, so the recorder's own
// backoff never fires) and recovers it by force re-registering the go2rtc stream
// and restarting the recorder on it. It also tracks last-seen and daily restart
// counts to surface in the health dashboard.
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

// Watchdog periodically checks recording liveness and self-heals.
type Watchdog struct {
	st  *store.Store
	sm  *stream.Manager
	rec Recorder
	log *slog.Logger

	mu            sync.Mutex
	state         map[string]*camState
	dayStamp      string
	onCameraState func(cameraID string, online bool) // change-feed camera.state hook
}

// New constructs a Watchdog.
func New(st *store.Store, sm *stream.Manager, rec Recorder, log *slog.Logger) *Watchdog {
	return &Watchdog{st: st, sm: sm, rec: rec, log: log, state: map[string]*camState{}}
}

// SetOnCameraState registers a callback invoked (async) with a camera's
// online/offline state on first observation and on every transition. Used to
// emit the change feed's camera.state (and seed its snapshot).
func (w *Watchdog) SetOnCameraState(fn func(cameraID string, online bool)) { w.onCameraState = fn }

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
	now := time.Now()
	day := now.UTC().Format("2006-01-02")

	w.mu.Lock()
	if day != w.dayStamp {
		w.dayStamp = day
		for _, s := range w.state {
			s.restartsToday = 0
		}
	}
	w.mu.Unlock()

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
		firstObs := s == nil
		if firstObs {
			s = &camState{}
			w.state[c.ID] = s
		}
		was := s.stale
		s.lastSeen = newest
		s.stale = stale
		// Emit camera.state on first observation (seeds the feed snapshot) and on
		// every online/offline transition.
		if w.onCameraState != nil && (firstObs || was != stale) {
			go w.onCameraState(c.ID, !stale)
		}
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
