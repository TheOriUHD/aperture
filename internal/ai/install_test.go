package ai

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tgz(t *testing.T, files map[string]string, links map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "pkg-1.0/", Typeflag: tar.TypeDir, Mode: 0o755})
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: "pkg-1.0/" + name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	for name, target := range links {
		_ = tw.WriteHeader(&tar.Header{Name: "pkg-1.0/" + name, Typeflag: tar.TypeSymlink, Linkname: target})
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestInstallUnpacksVerifiesAndResumes(t *testing.T) {
	archive := tgz(t,
		map[string]string{"lib/libonnxruntime.so.1.2.3": "elf", "include/x.h": "header", "LICENSE": "mit"},
		map[string]string{"lib/libonnxruntime.so": "libonnxruntime.so.1.2.3", "lib/evil.so": "/etc/passwd"},
	)
	model := []byte("onnx-model-bytes")
	var rangeSeen bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			rangeSeen = true
		}
		switch r.URL.Path {
		case "/rt.tgz":
			http.ServeContent(w, r, "rt.tgz", fileTime, bytes.NewReader(archive))
		case "/m.onnx":
			http.ServeContent(w, r, "m.onnx", fileTime, bytes.NewReader(model))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := &Pack{ID: "cpu", Lib: "onnxruntime/lib/libonnxruntime.so", Artifacts: []Artifact{{
		Name: "onnxruntime", Kind: "tgz", URL: srv.URL + "/rt.tgz", SHA256: sum(archive), Size: int64(len(archive)),
		Keep: []string{"lib/libonnxruntime.so*", "LICENSE"},
	}}}
	m := &Model{ID: "test", Name: "Test", Artifact: Artifact{Name: "test", Kind: "file", URL: srv.URL + "/m.onnx", SHA256: sum(model), Size: int64(len(model))}}

	s := Store{Dir: t.TempDir()}
	jobs := s.Missing(p, m)
	if len(jobs) != 2 || Bytes(jobs) != int64(len(archive)+len(model)) {
		t.Fatalf("missing = %d jobs, %d bytes", len(jobs), Bytes(jobs))
	}
	// A partial download from an earlier attempt is resumed, not restarted.
	_ = os.MkdirAll(filepath.Join(s.Dir, "tmp"), 0o755)
	partial := filepath.Join(s.Dir, "tmp", m.Artifact.SHA256[:16]+"-m.onnx")
	_ = os.WriteFile(partial, model[:5], 0o644)

	phases := map[string]int64{}
	if err := s.Install(context.Background(), jobs, func(p Progress) {
		if p.Total != Bytes(jobs) || p.Done > p.Total {
			t.Errorf("progress %+v", p)
		}
		phases[p.Phase] = max(phases[p.Phase], p.PartTotal)
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := phases["download"]; !ok || phases["unpack"] != int64(len(archive)) {
		t.Errorf("phases reported: %v", phases)
	}
	if !rangeSeen {
		t.Error("partial download was not resumed with a Range request")
	}
	if !s.PackInstalled(p) || !s.ModelInstalled(m) {
		t.Fatal("not marked installed")
	}
	if got, _ := os.Readlink(s.LibPath(p)); got != "libonnxruntime.so.1.2.3" {
		t.Fatalf("lib symlink = %q", got)
	}
	if _, err := os.Lstat(filepath.Join(s.Dir, "packs/cpu/onnxruntime/include/x.h")); !os.IsNotExist(err) {
		t.Error("kept a header that wasn't asked for")
	}
	if _, err := os.Lstat(filepath.Join(s.Dir, "packs/cpu/onnxruntime/lib/evil.so")); !os.IsNotExist(err) {
		t.Error("kept a symlink pointing outside the folder")
	}
	if h, _ := hashFile(s.ModelPath(m)); h != m.Artifact.SHA256 {
		t.Error("model content differs")
	}
	if len(s.Missing(p, m)) != 0 {
		t.Error("still missing after install")
	}
}

func TestInstallRejectsBadChecksum(t *testing.T) {
	body := []byte("tampered")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }))
	defer srv.Close()
	m := &Model{ID: "x", Name: "X", Artifact: Artifact{Name: "x", Kind: "file", URL: srv.URL + "/x", SHA256: sum([]byte("original")), Size: int64(len(body))}}
	s := Store{Dir: t.TempDir()}
	err := s.Install(context.Background(), s.Missing(nil, m), func(Progress) {})
	if err == nil {
		t.Fatal("installed a file that failed its checksum")
	}
	if s.ModelInstalled(m) {
		t.Fatal("marked installed")
	}
}

func TestDevicesOnAVirtualMachine(t *testing.T) {
	root := t.TempDir()
	old := sysRoot
	sysRoot = root
	defer func() { sysRoot = old }()
	write := func(p, s string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		_ = os.WriteFile(filepath.Join(root, p), []byte(s), 0o644)
	}
	write("proc/cpuinfo", "processor\t: 0\nmodel name\t: QEMU Virtual CPU version 2.5+\n")
	write("sys/bus/pci/devices/0000:00:01.0/class", "0x030000\n")
	write("sys/bus/pci/devices/0000:00:01.0/vendor", "0x1234\n") // the VM's own display

	devs := Devices(6)
	if len(devs) != 1 || devs[0].ID != "cpu" || !devs[0].Preferred {
		t.Fatalf("devices = %+v", devs)
	}
	if DefaultModel(devs[0]) != "yolox-tiny" {
		t.Fatalf("default model = %s", DefaultModel(devs[0]))
	}

	// A passed-through NVIDIA GPU without its driver is listed but not used.
	write("sys/bus/pci/devices/0000:01:00.0/class", "0x030000\n")
	write("sys/bus/pci/devices/0000:01:00.0/vendor", "0x10de\n")
	devs = Devices(6)
	if devs[0].ID != "cuda" || devs[0].Usable || devs[0].Reason == "" || PickDevice(devs, "auto").ID != "cpu" {
		t.Fatalf("GPU without driver: %+v", devs)
	}
	// With the driver it becomes the automatic choice.
	write("proc/driver/nvidia/version", "NVRM version: NVIDIA UNIX x86_64 Kernel Module  580.82.07  Thu Aug 14 2025\n")
	write("proc/driver/nvidia/gpus/0000:01:00.0/information", "Model: \t\t NVIDIA GeForce RTX 5060\nIRQ: 42\n")
	write("usr/lib/x86_64-linux-gnu/libcuda.so.1", "")
	devs = Devices(6)
	d := PickDevice(devs, "auto")
	if d.ID != "cuda" || d.Name != "NVIDIA GeForce RTX 5060" || d.Driver != "580.82.07" || DefaultModel(d) != "yolox-m" {
		t.Fatalf("GPU with driver: %+v", d)
	}
	if PickDevice(devs, "cpu").ID != "cpu" {
		t.Fatal("explicit CPU choice ignored")
	}
}

var fileTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// A GPU passed through after setup must not stop recognition: it keeps running
// on the CPU with what was downloaded, and the GPU is offered as an upgrade.
func TestGPUAddedAfterSetupKeepsCPURunning(t *testing.T) {
	root := t.TempDir()
	old := sysRoot
	sysRoot = root
	defer func() { sysRoot = old }()
	write := func(p, s string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		_ = os.WriteFile(filepath.Join(root, p), []byte(s), 0o644)
	}
	write("sys/bus/pci/devices/0000:01:00.0/class", "0x030000\n")
	write("sys/bus/pci/devices/0000:01:00.0/vendor", "0x10de\n")
	write("proc/driver/nvidia/version", "NVRM version: NVIDIA UNIX x86_64 Kernel Module  580.82.07\n")
	write("proc/driver/nvidia/gpus/0000:01:00.0/information", "Model: NVIDIA GeForce RTX 5060\n")
	write("usr/lib/x86_64-linux-gnu/libcuda.so.1", "")

	// The CPU setup is already downloaded.
	dir := t.TempDir()
	s := Store{Dir: dir}
	cpu := PackByID("cpu")
	for _, a := range cpu.Artifacts {
		d := s.artifactDir(cpu.ID, a)
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, ".sha256"), []byte(a.SHA256), 0o644)
	}
	tiny := ModelByID("yolox-tiny")
	_ = os.MkdirAll(filepath.Dir(s.ModelPath(tiny)), 0o755)
	_ = os.WriteFile(s.ModelPath(tiny)+".sha256", []byte(tiny.Artifact.SHA256), 0o644)

	m := NewManager(dir, "/nonexistent/apdetect", 3, slog.Default())
	m.Configure(&Settings{Device: "auto", Model: "auto"}, false)
	st := m.Status()
	if st.State == "needs-download" {
		t.Fatal("recognition stopped waiting for a GPU download")
	}
	if st.Upgrade == nil || st.Upgrade.ID != "cuda" || st.UpgradeDownload < 2<<30 {
		t.Fatalf("upgrade = %+v, %d bytes", st.Upgrade, st.UpgradeDownload)
	}
	m.Configure(nil, false)
}
