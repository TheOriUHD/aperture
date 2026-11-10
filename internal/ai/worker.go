package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// WorkerConfig says how to start an apdetect process.
type WorkerConfig struct {
	Bin      string   // apdetect executable
	Lib      string   // libonnxruntime.so
	Model    string   // .onnx file
	Provider string   // cpu | cuda
	Device   int      // GPU index
	Threads  int      // CPU threads per inference
	LibDirs  []string // added to LD_LIBRARY_PATH (CUDA libraries)
	MinScore float64  // lowest score the worker reports
	Packages string   // separate package model (.onnx), optional
	Arch     string   // main model format: yolox | detr
	Classes  string   // main model class list: a label set or a file (see apdetect -classes)
}

// ErrWorkerGone is returned for requests the worker can no longer answer.
var ErrWorkerGone = errors.New("detector stopped")

// Worker is a running apdetect process.
type Worker struct {
	cfg   WorkerConfig
	cmd   *exec.Cmd
	stdin io.WriteCloser
	hello Hello
	log   *slog.Logger

	wmu sync.Mutex // serialises frame writes

	mu      sync.Mutex
	nextID  uint32
	pending map[uint32]chan Result
	done    chan struct{}
	err     error
}

// StartWorker launches apdetect and waits for it to load the model (a GPU can
// take a while the first time, while it picks kernels).
func StartWorker(ctx context.Context, cfg WorkerConfig, log *slog.Logger) (*Worker, error) {
	args := []string{
		"-lib", cfg.Lib, "-model", cfg.Model, "-provider", cfg.Provider,
		"-device", strconv.Itoa(cfg.Device), "-threads", strconv.Itoa(max(1, cfg.Threads)),
		"-score", strconv.FormatFloat(cfg.MinScore, 'f', 3, 64),
	}
	if cfg.Packages != "" {
		args = append(args, "-packages", cfg.Packages)
	}
	if cfg.Arch != "" {
		args = append(args, "-arch", cfg.Arch)
	}
	if cfg.Classes != "" {
		args = append(args, "-classes", cfg.Classes)
	}
	cmd := exec.Command(cfg.Bin, args...)
	env := os.Environ()
	if len(cfg.LibDirs) > 0 {
		ld := strings.Join(cfg.LibDirs, ":")
		if cur := os.Getenv("LD_LIBRARY_PATH"); cur != "" {
			ld += ":" + cur
		}
		env = append(env, "LD_LIBRARY_PATH="+ld)
	}
	cmd.Env = env
	// The worker never outlives nvrd.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start detector: %w", err)
	}
	// If the service runs short of memory, the kernel should end the detector
	// (it restarts by itself) rather than the recorder: mark it first in line.
	_ = os.WriteFile(fmt.Sprintf("/proc/%d/oom_score_adj", cmd.Process.Pid), []byte("500"), 0o644)
	w := &Worker{cfg: cfg, cmd: cmd, stdin: stdin, log: log, pending: map[uint32]chan Result{}, done: make(chan struct{})}

	// Native libraries may print to stderr; keep the last lines for errors.
	var tail lastLines
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			tail.add(sc.Text())
			log.Debug("detector", "line", sc.Text())
		}
	}()

	rd := bufio.NewReaderSize(stdout, 256<<10)
	helloCh := make(chan error, 1)
	go func() {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			helloCh <- fmt.Errorf("detector exited while loading%s", tail.suffix())
			return
		}
		if err := json.Unmarshal(line, &w.hello); err != nil {
			helloCh <- fmt.Errorf("detector said %q", strings.TrimSpace(string(line)))
			return
		}
		if !w.hello.Ready {
			helloCh <- errors.New(w.hello.Error)
			return
		}
		helloCh <- nil
		w.readLoop(rd)
	}()
	go func() {
		err := cmd.Wait()
		w.mu.Lock()
		if w.err == nil {
			if err == nil {
				err = ErrWorkerGone
			}
			w.err = fmt.Errorf("%w: %v%s", ErrWorkerGone, err, tail.suffix())
		}
		for id, ch := range w.pending {
			ch <- Result{ID: id, Error: w.err.Error()}
			delete(w.pending, id)
		}
		w.mu.Unlock()
		close(w.done)
	}()

	timeout := 2 * time.Minute
	select {
	case err := <-helloCh:
		if err != nil {
			w.Close()
			return nil, err
		}
	case <-time.After(timeout):
		w.Close()
		return nil, errors.New("detector took too long to load the model")
	case <-ctx.Done():
		w.Close()
		return nil, ctx.Err()
	}
	return w, nil
}

func (w *Worker) readLoop(rd *bufio.Reader) {
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return
		}
		var res Result
		if json.Unmarshal(line, &res) != nil {
			continue
		}
		w.mu.Lock()
		ch := w.pending[res.ID]
		delete(w.pending, res.ID)
		w.mu.Unlock()
		if ch != nil {
			ch <- res
		}
	}
}

// Hello is what the worker reported when it became ready.
func (w *Worker) Hello() Hello { return w.hello }

// Done is closed when the worker process has exited.
func (w *Worker) Done() <-chan struct{} { return w.done }

// Err is why the worker exited (nil while it runs).
func (w *Worker) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

// Detect runs one packed-RGB frame through the main model.
func (w *Worker) Detect(ctx context.Context, rgb []byte, width, height int) (Result, error) {
	return w.DetectKind(ctx, KindObjects, rgb, width, height)
}

// DetectKind runs one packed-RGB frame through a model (KindObjects or
// KindPackages).
func (w *Worker) DetectKind(ctx context.Context, kind int, rgb []byte, width, height int) (Result, error) {
	ch := make(chan Result, 1)
	w.mu.Lock()
	if w.err != nil {
		err := w.err
		w.mu.Unlock()
		return Result{}, err
	}
	w.nextID++
	id := w.nextID
	w.pending[id] = ch
	w.mu.Unlock()

	w.wmu.Lock()
	err := WriteFrameKind(w.stdin, kind, id, width, height, rgb)
	w.wmu.Unlock()
	if err != nil {
		w.mu.Lock()
		delete(w.pending, id)
		w.mu.Unlock()
		return Result{}, fmt.Errorf("%w: %v", ErrWorkerGone, err)
	}
	select {
	case res := <-ch:
		if res.Error != "" {
			return res, errors.New(res.Error)
		}
		return res, nil
	case <-ctx.Done():
		w.mu.Lock()
		delete(w.pending, id)
		w.mu.Unlock()
		return Result{}, ctx.Err()
	}
}

// Close stops the worker.
func (w *Worker) Close() {
	_ = w.stdin.Close()
	select {
	case <-w.done:
	case <-time.After(3 * time.Second):
		_ = w.cmd.Process.Kill()
		<-w.done
	}
}

// lastLines keeps the last few stderr lines of the worker.
type lastLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *lastLines) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, s)
	if len(l.lines) > 6 {
		l.lines = l.lines[len(l.lines)-6:]
	}
}

func (l *lastLines) suffix() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.lines) == 0 {
		return ""
	}
	return ": " + strings.Join(l.lines, " | ")
}
