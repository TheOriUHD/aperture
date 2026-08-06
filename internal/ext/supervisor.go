package ext

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// rlimitNProc is RLIMIT_NPROC (Linux), which the standard syscall package does not
// define (it lives in x/sys/unix). We set it via the raw resource number to avoid
// pulling in a new dependency for one constant.
const rlimitNProc = 6

// Supervisor spawns and supervises backend-extension processes (T1.3). Each
// extension is a Deno process, confined by two layers: Deno's own permission
// flags (built by the caller from the manifest) and, if it escapes the runtime, a
// non-root per-extension uid + Pdeathsig + rlimits + the container's compose caps.
// nvrd itself stays root — required to setuid-drop each child; children never do.
//
// There is no prior supervised-process abstraction in the codebase (the recorder
// SIGKILLs ffmpeg and discards its output); this is new. It captures child output,
// restarts on crash with backoff, gives up after repeated fast crashes, stops
// gracefully (SIGTERM → WaitDelay → SIGKILL), and drains all children on shutdown.
type Supervisor struct {
	denoPath string
	selfExe  string // for the rlimit trampoline (/proc/self/exe)
	log      *slog.Logger

	mu       sync.Mutex
	children map[string]*child

	// argv builds the (path, args) to launch; the default runs Deno. Tests
	// override it to launch a stand-in command without the real Deno binary.
	argv func(Spec) (string, []string)

	// restart tuning (fields, not consts, so tests can shrink them).
	maxFastCrashes int
	fastCrashUnder time.Duration
	backoffBase    time.Duration
	backoffMax     time.Duration
}

type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopped  State = "stopped"
	StateFailed   State = "failed"
)

const (
	stopGrace    = 5 * time.Second // SIGTERM → SIGKILL window
	logRingLines = 200
)

// Spec is a resolved launch request. Entry, DenoFlags, Env, and the ext data dir
// are computed by the caller (install/handlers) from the validated manifest.
type Spec struct {
	ID        string
	Entry     string   // absolute path to the backend entry (.ts)
	DenoFlags []string // --allow-net=…, --allow-read=…, --deny-run, --deny-ffi, --import-map=…, etc.
	Env       []string // minimal: APERTURE_TOKEN, APERTURE_API
	WorkDir   string   // the extension's own data dir (cwd, --allow-write target)
	UID, GID  int      // per-extension non-root ids; 0 = do not drop (tests)
	NOFILE    uint64   // RLIMIT_NOFILE (0 = leave default)
	NPROC     uint64   // RLIMIT_NPROC  (0 = leave default)
}

type child struct {
	id     string
	ctx    context.Context
	cancel context.CancelFunc
	logs   *lineRing

	mu       sync.Mutex
	state    State
	reason   string
	restarts int
}

func NewSupervisor(denoPath string, log *slog.Logger) *Supervisor {
	s := &Supervisor{
		denoPath: denoPath, selfExe: "/proc/self/exe", log: log, children: map[string]*child{},
		maxFastCrashes: 5, fastCrashUnder: 30 * time.Second, backoffBase: time.Second, backoffMax: 30 * time.Second,
	}
	s.argv = s.denoArgv
	return s
}

// denoArgv is the production launcher: `deno run <flags> <entry>`.
func (s *Supervisor) denoArgv(spec Spec) (string, []string) {
	return s.denoPath, append(append([]string{"run"}, spec.DenoFlags...), spec.Entry)
}

// Start launches an extension and supervises it in the background. If it is
// already running (or starting) this is a no-op; a previously stopped or failed
// child is replaced with a fresh run.
func (s *Supervisor) Start(spec Spec) {
	s.mu.Lock()
	if c, ok := s.children[spec.ID]; ok {
		if st, _ := c.get(); st == StateRunning || st == StateStarting {
			s.mu.Unlock()
			return
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &child{id: spec.ID, ctx: ctx, cancel: cancel, logs: newLineRing(logRingLines), state: StateStarting}
	s.children[spec.ID] = c
	s.mu.Unlock()
	go s.supervise(c, spec)
}

// Stop gracefully stops an extension (SIGTERM → WaitDelay → SIGKILL) but keeps its
// row so its state reads `stopped` and its logs remain readable (this backs the
// manager's disable). Safe on an unknown id.
func (s *Supervisor) Stop(id string) {
	s.mu.Lock()
	c := s.children[id]
	s.mu.Unlock()
	if c != nil {
		c.cancel()
	}
}

// Remove stops an extension and drops it from the registry entirely (uninstall).
func (s *Supervisor) Remove(id string) {
	s.mu.Lock()
	c := s.children[id]
	delete(s.children, id)
	s.mu.Unlock()
	if c != nil {
		c.cancel()
	}
}

// StopAll drains every child — called before nvrd cancels its background context
// on shutdown, so extensions get their SIGTERM window instead of a bare SIGKILL.
// Children are left in the registry (the process is exiting anyway).
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	cs := make([]*child, 0, len(s.children))
	for _, c := range s.children {
		cs = append(cs, c)
	}
	s.mu.Unlock()
	for _, c := range cs {
		c.cancel()
	}
}

// Status reports an extension's current state and, if failed, why.
func (s *Supervisor) Status(id string) (State, string) {
	s.mu.Lock()
	c := s.children[id]
	s.mu.Unlock()
	if c == nil {
		return StateStopped, ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state, c.reason
}

// Logs returns the last captured stdout/stderr lines for an extension.
func (s *Supervisor) Logs(id string) []string {
	s.mu.Lock()
	c := s.children[id]
	s.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.logs.snapshot()
}

func (s *Supervisor) supervise(c *child, spec Spec) {
	backoff := s.backoffBase
	fastCrashes := 0
	for {
		if c.ctx.Err() != nil {
			c.set(StateStopped, "")
			return
		}
		cmd := s.buildCmd(c.ctx, spec)
		cmd.Stdout = &lineWriter{c: c, stream: "stdout", log: s.log}
		cmd.Stderr = &lineWriter{c: c, stream: "stderr", log: s.log}

		c.set(StateRunning, "")
		start := time.Now()
		err := cmd.Run()
		ran := time.Since(start)

		if c.ctx.Err() != nil { // intentional stop
			c.set(StateStopped, "")
			return
		}
		// Crash.
		c.bumpRestarts()
		if ran >= s.fastCrashUnder {
			fastCrashes, backoff = 0, s.backoffBase
		} else {
			fastCrashes++
			if backoff *= 2; backoff > s.backoffMax {
				backoff = s.backoffMax
			}
		}
		if fastCrashes >= s.maxFastCrashes {
			reason := fmt.Sprintf("keeps crashing (%d fast crashes); last exit: %v", fastCrashes, err)
			c.logs.add("[supervisor] giving up: " + reason)
			s.log.Warn("extension gave up", "id", spec.ID, "err", err)
			c.set(StateFailed, reason)
			return
		}
		c.logs.add(fmt.Sprintf("[supervisor] exited after %s (%v); restarting in %s", ran.Round(time.Millisecond), err, backoff))
		c.set(StateStarting, "")
		select {
		case <-c.ctx.Done():
			c.set(StateStopped, "")
			return
		case <-time.After(backoff):
		}
	}
}

// buildCmd assembles the exec.Cmd. When rlimits are requested it launches through
// the in-binary trampoline (/proc/self/exe __ext-spawn) which applies setrlimit
// as the already-dropped child, then execs Deno. Privilege drop (Credential), own
// process group (Setpgid), and Pdeathsig are set here; graceful stop is SIGTERM to
// the group then SIGKILL after WaitDelay.
func (s *Supervisor) buildCmd(ctx context.Context, spec Spec) *exec.Cmd {
	path, args := s.argv(spec)

	var cmd *exec.Cmd
	if spec.NOFILE > 0 || spec.NPROC > 0 {
		rl, _ := json.Marshal(map[string]uint64{"nofile": spec.NOFILE, "nproc": spec.NPROC})
		wrapped := append([]string{spawnSubcommand, string(rl), "--", path}, args...)
		cmd = exec.CommandContext(ctx, s.selfExe, wrapped...)
	} else {
		cmd = exec.CommandContext(ctx, path, args...)
	}
	cmd.Dir = spec.WorkDir
	cmd.Env = spec.Env

	spa := &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if spec.UID > 0 {
		spa.Credential = &syscall.Credential{Uid: uint32(spec.UID), Gid: uint32(spec.GID)}
	}
	cmd.SysProcAttr = spa

	// Graceful stop: signal the whole process group, escalate after the window.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		return nil
	}
	cmd.WaitDelay = stopGrace
	return cmd
}

// lineWriter splits child stdout/stderr into lines, appending each to the
// per-extension ring and re-emitting it into nvrd's slog (so it also lands in the
// diagnostics bundle). Set as cmd.Stdout/Stderr so it is race-free with cmd.Run().
type lineWriter struct {
	c      *child
	stream string
	log    *slog.Logger
	buf    []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		w.c.logs.add(line)
		w.log.Info("ext", "id", w.c.id, "stream", w.stream, "line", line)
	}
	return len(p), nil
}

func (c *child) set(st State, reason string) {
	c.mu.Lock()
	c.state, c.reason = st, reason
	c.mu.Unlock()
}

func (c *child) get() (State, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state, c.reason
}

func (c *child) bumpRestarts() {
	c.mu.Lock()
	c.restarts++
	c.mu.Unlock()
}

// ---- per-extension log ring --------------------------------------------------

type lineRing struct {
	mu    sync.Mutex
	buf   []string
	max   int
	start int
	n     int
}

func newLineRing(max int) *lineRing { return &lineRing{buf: make([]string, max), max: max} }

func (r *lineRing) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := (r.start + r.n) % r.max
	if r.n < r.max {
		r.n++
	} else {
		r.start = (r.start + 1) % r.max
	}
	r.buf[idx] = line
}

func (r *lineRing) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, r.n)
	for i := 0; i < r.n; i++ {
		out[i] = r.buf[(r.start+i)%r.max]
	}
	return out
}

// ---- rlimit trampoline -------------------------------------------------------

const spawnSubcommand = "__ext-spawn"

// RunSpawnTrampoline must be called at the very top of main(). When nvrd re-execs
// itself as `nvrd __ext-spawn <rlimit-json> -- <deno> <args…>` (already dropped to
// the extension's uid by the parent's Credential), this applies the requested
// rlimits to the current process and execs Deno in place. Lowering soft limits is
// permitted for an unprivileged process, so no privilege is needed here. It never
// returns on the spawn path.
func RunSpawnTrampoline() {
	if len(os.Args) < 2 || os.Args[1] != spawnSubcommand {
		return
	}
	// os.Args: [nvrd, __ext-spawn, <rlimit-json>, --, deno, run, …]
	var rl map[string]uint64
	if len(os.Args) >= 3 {
		_ = json.Unmarshal([]byte(os.Args[2]), &rl)
	}
	sep := -1
	for i, a := range os.Args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep+1 >= len(os.Args) {
		fmt.Fprintln(os.Stderr, "ext spawn: malformed trampoline invocation")
		os.Exit(127)
	}
	if v, ok := rl["nofile"]; ok && v > 0 {
		_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{Cur: v, Max: v})
	}
	if v, ok := rl["nproc"]; ok && v > 0 {
		_ = syscall.Setrlimit(rlimitNProc, &syscall.Rlimit{Cur: v, Max: v})
	}
	cmd := os.Args[sep+1:]
	if err := syscall.Exec(cmd[0], cmd, os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, "ext spawn: exec "+cmd[0]+": "+err.Error())
		os.Exit(127)
	}
}
