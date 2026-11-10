package ai

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ulikunitz/xz"
)

// Progress describes a running (or failed) download.
type Progress struct {
	Active bool   `json:"active"`
	Stage  string `json:"stage,omitempty"` // what is happening, in plain words
	Phase  string `json:"phase,omitempty"` // download | unpack
	Done   int64  `json:"done"`            // bytes downloaded so far
	Total  int64  `json:"total"`           // bytes to download in all
	// Rate is the current speed in bytes a second: received while
	// downloading, written to disk while unpacking (0 until measured).
	Rate int64 `json:"rate,omitempty"`
	// While unpacking: how much of the archive is read, and its size.
	Part      int64  `json:"part,omitempty"`
	PartTotal int64  `json:"partTotal,omitempty"`
	Error     string `json:"error,omitempty"`
}

// rateMeter measures a speed over the last few seconds.
type rateMeter struct {
	at []time.Time
	n  []int64
}

// add records that n bytes are done by now and returns the speed.
func (r *rateMeter) add(now time.Time, n int64) int64 {
	r.at = append(r.at, now)
	r.n = append(r.n, n)
	for len(r.at) > 2 && now.Sub(r.at[0]) > 3*time.Second {
		r.at, r.n = r.at[1:], r.n[1:]
	}
	dt := now.Sub(r.at[0]).Seconds()
	if dt < 0.5 {
		return 0
	}
	return int64(float64(n-r.n[0]) / dt)
}

// throttled calls fn at most every 250 ms, with the speed of the bytes it is
// given.
func throttled(fn func(n, rate int64)) func(n int64) {
	var meter rateMeter
	var last time.Time
	return func(n int64) {
		now := time.Now()
		if now.Sub(last) < 250*time.Millisecond {
			return
		}
		last = now
		fn(n, meter.add(now, n))
	}
}

// Store is the folder holding downloaded runtimes and models:
//
//	packs/<pack>/<artifact>/…   unpacked libraries, with a .sha256 marker
//	models/<model>.onnx         with a .sha256 marker
//	tmp/                        partial downloads
type Store struct {
	Dir string
}

func (s Store) artifactDir(pack string, a Artifact) string {
	return filepath.Join(s.Dir, "packs", pack, a.Name)
}

func (s Store) modelPath(m *Model) string { return filepath.Join(s.Dir, "models", m.ID+".onnx") }

// installed reports whether a finished install of exactly this file is there.
func installed(marker, sha string) bool {
	b, err := os.ReadFile(marker)
	return err == nil && strings.TrimSpace(string(b)) == sha
}

// PackInstalled reports whether every part of a pack is installed.
func (s Store) PackInstalled(p *Pack) bool {
	for _, a := range p.Artifacts {
		if !installed(filepath.Join(s.artifactDir(p.ID, a), ".sha256"), a.SHA256) {
			return false
		}
	}
	return true
}

// ModelInstalled reports whether a model is installed.
func (s Store) ModelInstalled(m *Model) bool {
	return installed(s.modelPath(m)+".sha256", m.Artifact.SHA256)
}

// LibPath is a pack's libonnxruntime.so.
func (s Store) LibPath(p *Pack) string { return filepath.Join(s.Dir, "packs", p.ID, p.Lib) }

// LibDirs are a pack's library folders, absolute.
func (s Store) LibDirs(p *Pack) []string {
	out := make([]string, 0, len(p.LibDirs))
	for _, d := range p.LibDirs {
		out = append(out, filepath.Join(s.Dir, "packs", p.ID, d))
	}
	return out
}

// ModelPath is a model's .onnx file.
func (s Store) ModelPath(m *Model) string { return s.modelPath(m) }

// job is one download.
type job struct {
	label string
	a     Artifact
	dest  string // folder (archive) or file path (Kind "file")
}

// Missing lists what must still be downloaded for a pack and models (nil
// models are skipped).
func (s Store) Missing(p *Pack, models ...*Model) []job {
	var jobs []job
	if p != nil {
		for _, a := range p.Artifacts {
			if !installed(filepath.Join(s.artifactDir(p.ID, a), ".sha256"), a.SHA256) {
				jobs = append(jobs, job{label: artifactLabel(a.Name), a: a, dest: s.artifactDir(p.ID, a)})
			}
		}
	}
	for _, m := range models {
		if m != nil && !s.ModelInstalled(m) {
			label := "the " + m.Name + " detection model"
			if isPackageModel(m.Artifact) {
				label = "the package model"
			}
			jobs = append(jobs, job{label: label, a: m.Artifact, dest: s.modelPath(m)})
		}
	}
	return jobs
}

func isPackageModel(a Artifact) bool {
	for _, pm := range PackageModels {
		if pm.Artifact.SHA256 == a.SHA256 {
			return true
		}
	}
	return false
}

// onlyPackageModels reports whether jobs fetch nothing but package models.
func onlyPackageModels(jobs []job) bool {
	for _, j := range jobs {
		if !isPackageModel(j.a) {
			return false
		}
	}
	return len(jobs) > 0
}

// updating reports whether any job replaces an older installed version.
func (s Store) updating(jobs []job) bool {
	for _, j := range jobs {
		if j.a.Kind == "file" {
			continue
		}
		if _, err := os.Stat(filepath.Join(j.dest, ".sha256")); err == nil {
			return true
		}
	}
	return false
}

// Bytes is the download size of a set of jobs.
func Bytes(jobs []job) int64 {
	var n int64
	for _, j := range jobs {
		n += j.a.Size
	}
	return n
}

func artifactLabel(name string) string {
	switch name {
	case "onnxruntime":
		return "ONNX Runtime"
	case "cudart":
		return "the CUDA runtime"
	case "cublas":
		return "NVIDIA cuBLAS"
	case "cufft":
		return "NVIDIA cuFFT"
	case "curand":
		return "NVIDIA cuRAND"
	case "nvrtc":
		return "NVIDIA NVRTC"
	case "nvjitlink":
		return "NVIDIA nvJitLink"
	case "cudnn":
		return "NVIDIA cuDNN"
	}
	return name
}

// FreeBytes is the free space where the store lives.
func (s Store) FreeBytes() int64 {
	dir := s.Dir
	for {
		var st syscall.Statfs_t
		if err := syscall.Statfs(dir, &st); err == nil {
			return int64(st.Bavail) * int64(st.Bsize)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return -1
		}
		dir = parent
	}
}

// Install downloads, verifies and unpacks jobs in order, reporting progress.
func (s Store) Install(ctx context.Context, jobs []job, report func(Progress)) error {
	total := Bytes(jobs)
	// Unpacked libraries take about twice their archives; keep a margin.
	if free := s.FreeBytes(); free >= 0 && free < total*3 {
		return fmt.Errorf("not enough disk space: %s needed, %s free", humanBytes(total*3), humanBytes(free))
	}
	if err := os.MkdirAll(filepath.Join(s.Dir, "tmp"), 0o755); err != nil {
		return err
	}
	var before int64
	for _, j := range jobs {
		stage := "Downloading " + j.label
		onBytes := throttled(func(n, rate int64) {
			report(Progress{Active: true, Stage: stage, Phase: "download", Done: before + n, Total: total, Rate: rate})
		})
		file, err := s.download(ctx, j.a, onBytes)
		if err != nil {
			return fmt.Errorf("download %s: %w", j.label, err)
		}
		unpacking := Progress{Active: true, Stage: "Unpacking " + j.label, Phase: "unpack", Done: before + j.a.Size, Total: total, PartTotal: j.a.Size}
		if j.a.Kind == "file" {
			unpacking.Stage = "Saving " + j.label
		}
		report(unpacking)
		var read int64
		onWritten := throttled(func(n, rate int64) {
			p := unpacking
			p.Part, p.Rate = min(read, j.a.Size), rate
			report(p)
		})
		onUnpack := func(r, written int64) {
			read = r
			onWritten(written)
		}
		if err := s.place(ctx, file, j, onUnpack); err != nil {
			return fmt.Errorf("unpack %s: %w", j.label, err)
		}
		_ = os.Remove(file)
		before += j.a.Size
	}
	_ = os.RemoveAll(filepath.Join(s.Dir, "tmp"))
	return nil
}

// download fetches an artifact into tmp/, resuming a partial file, and checks
// its SHA-256. It returns the path of the verified file.
func (s Store) download(ctx context.Context, a Artifact, onBytes func(int64)) (string, error) {
	path := filepath.Join(s.Dir, "tmp", a.SHA256[:16]+"-"+filepath.Base(a.URL))
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * 2 * time.Second):
			}
		}
		err := fetch(ctx, a, path, onBytes)
		if err == nil {
			return path, nil
		}
		if errors.Is(err, errChecksum) || ctx.Err() != nil {
			_ = os.Remove(path)
			return "", err
		}
		lastErr = err
	}
	return "", lastErr
}

var errChecksum = errors.New("the file did not match its published checksum")

func fetch(ctx context.Context, a Artifact, path string, onBytes func(int64)) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	// Re-hash what a previous attempt already wrote, then resume after it.
	h := sha256.New()
	have, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if have > a.Size {
		have = 0
		h.Reset()
		if err := f.Truncate(0); err != nil {
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}
	if have < a.Size {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "Aperture-NVR")
		if have > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		switch {
		case have > 0 && resp.StatusCode == http.StatusPartialContent:
		case resp.StatusCode == http.StatusOK:
			if have > 0 { // server ignored the range: start over
				have = 0
				h.Reset()
				if err := f.Truncate(0); err != nil {
					return err
				}
				if _, err := f.Seek(0, io.SeekStart); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("server answered %s", resp.Status)
		}
		w := &countingWriter{w: io.MultiWriter(f, h), n: have, on: onBytes}
		if _, err := io.Copy(w, io.LimitReader(resp.Body, a.Size-have)); err != nil {
			return err
		}
		have = w.n
	}
	if have != a.Size {
		return fmt.Errorf("incomplete download (%d of %d bytes)", have, a.Size)
	}
	if hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return errChecksum
	}
	return nil
}

type countingWriter struct {
	w    io.Writer
	n    int64
	on   func(total int64)
	each func(n int) // optional: told each write's size
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	c.on(c.n)
	if c.each != nil {
		c.each(n)
	}
	return n, err
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// place moves a verified download into its final place: unpacking archives
// into a fresh folder, then swapping it in and writing the marker last.
// progress is told how much of the archive is read and how much is written.
func (s Store) place(ctx context.Context, file string, j job, progress func(read, written int64)) error {
	if j.a.Kind == "file" {
		if err := os.MkdirAll(filepath.Dir(j.dest), 0o755); err != nil {
			return err
		}
		if err := copyFile(file, j.dest+".part"); err != nil {
			return err
		}
		if err := os.Rename(j.dest+".part", j.dest); err != nil {
			return err
		}
		return os.WriteFile(j.dest+".sha256", []byte(j.a.SHA256+"\n"), 0o644)
	}
	stage := j.dest + ".new"
	_ = os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	if err := unpack(ctx, file, j.a, stage, progress); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	_ = os.RemoveAll(j.dest)
	if err := os.Rename(stage, j.dest); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(j.dest, ".sha256"), []byte(j.a.SHA256+"\n"), 0o644)
}

// unpack extracts the files of an archive matching a.Keep into dest, dropping
// the archive's top folder. Symlinks may only point at a sibling file.
func unpack(ctx context.Context, file string, a Artifact, dest string, progress func(read, written int64)) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	in := &countingReader{r: f}
	var written int64
	tell := func(n int) {
		written += int64(n)
		progress(in.n, written)
	}
	var r io.Reader
	switch a.Kind {
	case "tgz":
		gz, err := gzip.NewReader(in)
		if err != nil {
			return err
		}
		defer gz.Close()
		r = gz
	case "txz":
		xr, err := xz.NewReader(in)
		if err != nil {
			return err
		}
		r = xr
	default:
		return fmt.Errorf("unknown archive kind %q", a.Kind)
	}
	tr := tar.NewReader(r)
	kept := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := path.Clean(hdr.Name)
		if i := strings.IndexByte(name, '/'); i >= 0 {
			name = name[i+1:]
		} else {
			continue // the top folder itself
		}
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") || !keep(a.Keep, name) {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeReg:
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(&countingWriter{w: out, on: func(int64) {}, each: tell}, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
			kept++
		case tar.TypeSymlink:
			if strings.Contains(hdr.Linkname, "/") {
				continue
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
			kept++
		}
	}
	if kept == 0 {
		return errors.New("the archive held none of the expected files")
	}
	return nil
}

func keep(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// hashFile is the SHA-256 of a file (used by tests).
func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var h hash.Hash = sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%d KB", n/1024)
	}
}
