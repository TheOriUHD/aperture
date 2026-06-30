// Package watchdog supervises camera health: it detects a camera whose recording
// has gone stale (a wedged producer that hasn't exited, so the recorder's own
// backoff never fires) and recovers it by force re-registering the go2rtc stream
// — tearing down the wedged producer so the recorder reconnects. It also tracks
// last-seen and daily restart counts to surface in the health dashboard.
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
	log *slog.Logger

	mu       sync.Mutex
	state    map[string]*camState
	dayStamp string
}

// New constructs a Watchdog.
func New(st *store.Store, sm *stream.Manager, log *slog.Logger) *Watchdog {
	return &Watchdog{st: st, sm: sm, log: log, state: map[string]*camState{}}
}

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
		stale := newest > 0 && nowMs-newest > store.RecordingGraceMs

		w.mu.Lock()
		s := w.state[c.ID]
		if s == nil {
			s = &camState{}
			w.state[c.ID] = s
		}
		s.lastSeen = newest
		s.stale = stale
		needRecover := stale && time.Since(s.lastRecover) > recoverBackoff
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
