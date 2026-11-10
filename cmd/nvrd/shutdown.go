package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Shutdown budget. Each step is bounded so the whole sequence (about 37s at
// worst, normally a second or two) stays inside the unit's TimeoutStopSec=45:
// past that, systemd SIGKILLs the cgroup, recorders included.
const (
	extStopSlack = 2 * time.Second  // on top of the supervisor's SIGTERM → SIGKILL window
	bgStopWait   = 20 * time.Second // recorder ffmpeg: ≤10s each in parallel, plus a pass in flight
	httpStopWait = 10 * time.Second // ordinary requests in flight (event streams end at once)
)

// services is the set of background loops that shutdown must wait for, rather
// than abandon to process exit.
type services struct {
	ctx  context.Context
	list []*service
}

type service struct {
	name string
	done chan struct{}
	end  time.Time // when fn returned; read only once done is closed
}

// run starts fn(ctx) in a goroutine and tracks its return.
func (s *services) run(name string, fn func(context.Context)) {
	svc := &service{name: name, done: make(chan struct{})}
	go func() {
		fn(s.ctx)
		svc.end = time.Now()
		close(svc.done)
	}()
	s.list = append(s.list, svc)
}

// slowest names the service that returned last, and how long after since: the
// one that held up the shutdown, if anything did.
func (s *services) slowest(since time.Time) (name string, took time.Duration) {
	for _, svc := range s.list {
		select {
		case <-svc.done:
			if d := svc.end.Sub(since); d > took {
				name, took = svc.name, d
			}
		default:
		}
	}
	return name, took
}

// wait blocks until every service has returned or timeout passes, and reports
// the ones still running.
func (s *services) wait(timeout time.Duration) (pending []string) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for i, svc := range s.list {
		select {
		case <-svc.done:
		case <-deadline.C:
			for _, rest := range s.list[i:] {
				select {
				case <-rest.done:
				default:
					pending = append(pending, rest.name)
				}
			}
			return pending
		}
	}
	return nil
}

// shutdownHTTP stops the API server: no new connections, event streams ended
// (api.NewHTTPServer registers that), and requests already running given
// timeout to finish. One that is still going after that — a long export
// download, say — is cut rather than allowed to hold up the restart.
func shutdownHTTP(hs *http.Server, timeout time.Duration, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := hs.Shutdown(ctx); err != nil {
		log.Warn("http shutdown: requests still running, closing their connections", "after", timeout, "err", err)
		_ = hs.Close()
	}
}
