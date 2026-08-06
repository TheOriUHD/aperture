package ext

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func newTestSupervisor() *Supervisor {
	s := NewSupervisor("deno-unused", slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Launch a shell running the Entry as a script instead of Deno, so the
	// lifecycle is exercised without the real runtime (its confinement is verified
	// against the deployed image). Shrink the crash-loop timing so give-up is fast.
	s.argv = func(spec Spec) (string, []string) { return "sh", []string{"-c", spec.Entry} }
	s.maxFastCrashes, s.fastCrashUnder = 3, time.Second
	s.backoffBase, s.backoffMax = 5*time.Millisecond, 20*time.Millisecond
	return s
}

func waitState(t *testing.T, s *Supervisor, id string, want State, within time.Duration) {
	t.Helper()
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if st, _ := s.Status(id); st == want {
			return
		}
	}
	st, reason := s.Status(id)
	t.Fatalf("%s state=%q(%q), want %q within %s", id, st, reason, want, within)
}

func TestSupervisorRunStopAndLogs(t *testing.T) {
	s := newTestSupervisor()
	s.Start(Spec{ID: "a", Entry: "echo ready; sleep 30"})
	waitState(t, s, "a", StateRunning, 3*time.Second)

	got := false
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline) && !got; time.Sleep(5 * time.Millisecond) {
		for _, l := range s.Logs("a") {
			if strings.Contains(l, "ready") {
				got = true
			}
		}
	}
	if !got {
		t.Fatalf("expected 'ready' in captured logs, got %v", s.Logs("a"))
	}

	// Stop leaves the child observably stopped — the state is set only after the
	// process actually exits (SIGTERM to the group kills both sh and sleep).
	s.Stop("a")
	waitState(t, s, "a", StateStopped, 3*time.Second)
}

func TestSupervisorCrashLoopGivesUp(t *testing.T) {
	s := newTestSupervisor()
	s.Start(Spec{ID: "b", Entry: "exit 7"})
	waitState(t, s, "b", StateFailed, 3*time.Second)
	if _, reason := s.Status("b"); !strings.Contains(reason, "keeps crashing") {
		t.Fatalf("failed reason = %q, want it to mention crashing", reason)
	}
}

func TestSupervisorStopAllDrains(t *testing.T) {
	s := newTestSupervisor()
	s.Start(Spec{ID: "c", Entry: "sleep 30"})
	s.Start(Spec{ID: "d", Entry: "sleep 30"})
	waitState(t, s, "c", StateRunning, 3*time.Second)
	waitState(t, s, "d", StateRunning, 3*time.Second)
	s.StopAll()
	waitState(t, s, "c", StateStopped, 3*time.Second)
	waitState(t, s, "d", StateStopped, 3*time.Second)
}
