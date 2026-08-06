package ext

import (
	"context"
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

func TestDenoArgvInjectsMemCap(t *testing.T) {
	s := NewSupervisor("/usr/local/bin/deno", slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, args := s.denoArgv(Spec{Entry: "/x/main.ts", MemMB: 256, DenoFlags: []string{"--deny-run"}})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--v8-flags=--max-old-space-size=256") {
		t.Fatalf("expected V8 heap cap flag, got: %s", joined)
	}
	if !strings.Contains(joined, "--deny-run") || args[len(args)-1] != "/x/main.ts" {
		t.Fatalf("flags/entry order wrong: %s", joined)
	}
}

func TestBuildCmdUsesTrampolineWhenLimited(t *testing.T) {
	s := NewSupervisor("/usr/local/bin/deno", slog.New(slog.NewTextHandler(io.Discard, nil)))
	cmd := s.buildCmd(context.Background(), Spec{Entry: "/x/main.ts", NOFILE: 256, OOMScoreAdj: 800})
	if cmd.Path != s.selfExe {
		t.Fatalf("limited spawn should go via the trampoline (%s), got %s", s.selfExe, cmd.Path)
	}
	if joined := strings.Join(cmd.Args, " "); !strings.Contains(joined, spawnSubcommand) || !strings.Contains(joined, `"oom":800`) {
		t.Fatalf("trampoline args missing subcommand/oom: %s", joined)
	}
	if cmd2 := s.buildCmd(context.Background(), Spec{Entry: "/x/main.ts"}); cmd2.Path != s.denoPath {
		t.Fatalf("unlimited spawn should exec deno directly, got %s", cmd2.Path)
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
