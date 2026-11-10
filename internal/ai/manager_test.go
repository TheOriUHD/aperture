package ai

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeNVIDIA makes the device scan see an RTX 5060 with a working driver.
func fakeNVIDIA(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	old := sysRoot
	sysRoot = root
	t.Cleanup(func() { sysRoot = old })
	write := func(p, s string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		_ = os.WriteFile(filepath.Join(root, p), []byte(s), 0o644)
	}
	write("sys/bus/pci/devices/0000:01:00.0/class", "0x030000\n")
	write("sys/bus/pci/devices/0000:01:00.0/vendor", "0x10de\n")
	write("proc/driver/nvidia/version", "NVRM version: NVIDIA UNIX Open Kernel Module for x86_64  620.70.06\n")
	write("proc/driver/nvidia/gpus/0000:01:00.0/information", "Model: NVIDIA GeForce RTX 5060\n")
	write("usr/lib/x86_64-linux-gnu/libcuda.so.1", "")
}

// markCPUSetup pretends the CPU runtime and the tiny model are downloaded.
func markCPUSetup(s Store) {
	cpu := PackByID("cpu")
	for _, a := range cpu.Artifacts {
		d := s.artifactDir(cpu.ID, a)
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, ".sha256"), []byte(a.SHA256), 0o644)
	}
	tiny := ModelByID("yolox-tiny")
	_ = os.MkdirAll(filepath.Dir(s.ModelPath(tiny)), 0o755)
	_ = os.WriteFile(s.ModelPath(tiny)+".sha256", []byte(tiny.Artifact.SHA256), 0o644)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Switching to a GPU is a 3 GB download; recognition on the CPU must go on
// meanwhile, and come back as it was if the download is cancelled.
func TestCPUKeepsRecognisingWhileTheGPUDownloads(t *testing.T) {
	fakeNVIDIA(t)
	dir := t.TempDir()
	markCPUSetup(Store{Dir: dir})

	// A stand-in detector: says it's ready, then takes frames.
	bin := filepath.Join(t.TempDir(), "apdetect")
	script := "#!/bin/sh\necho '{\"ready\":true,\"provider\":\"cpu\",\"version\":\"test\",\"input\":416}'\nexec cat >/dev/null\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// The GPU runtime comes from a server that sends a little, then stalls.
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 4096))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	defer srv.Close()
	defer close(stop)
	cuda := PackByID("cuda")
	saved := cuda.Artifacts
	cuda.Artifacts = []Artifact{{Name: "onnxruntime", Kind: "tgz", URL: srv.URL + "/rt.tgz", SHA256: sum([]byte("x")), Size: 1 << 20}}
	defer func() { cuda.Artifacts = saved }()

	m := NewManager(dir, bin, 3, slog.Default())
	defer stopManager(m)
	auto := Settings{Device: "auto", Model: "auto"}
	m.Configure(&auto, false)
	waitFor(t, "the CPU detector", func() bool { return m.Status().State == "running" })
	if st := m.Status(); st.Upgrade == nil || st.Upgrade.ID != "cuda" {
		t.Fatalf("no GPU offered: %+v", st.Upgrade)
	}

	m.Configure(&auto, true) // "Use the GPU & download"
	waitFor(t, "download progress", func() bool { p := m.Status().Install; return p.Phase == "download" && p.Done > 0 })
	st := m.Status()
	if st.State != "installing" || !m.Running() || st.Provider != "cpu" {
		t.Fatalf("during the download: state %s, running %v, provider %q", st.State, m.Running(), st.Provider)
	}
	// Saving a camera re-applies the same settings: the download carries on.
	m.Configure(&auto, false)
	if st := m.Status(); st.State != "installing" || !st.Install.Active {
		t.Fatalf("re-applying the settings disturbed the download: %s", st.State)
	}

	m.Cancel()
	waitFor(t, "the cancel", func() bool { return m.Status().State == "running" })
	st = m.Status()
	if !m.Running() || st.Upgrade == nil || st.Install.Active {
		t.Fatalf("after cancelling: running %v, upgrade %+v, install %+v", m.Running(), st.Upgrade, st.Install)
	}
}

func TestRateMeter(t *testing.T) {
	var r rateMeter
	t0 := time.Unix(1000, 0)
	if got := r.add(t0, 0); got != 0 {
		t.Fatalf("first sample = %d", got)
	}
	if got := r.add(t0.Add(250*time.Millisecond), 1<<20); got != 0 {
		t.Fatalf("too early to tell, got %d", got)
	}
	// 10 MB/s, steady: the old samples drop out of the window.
	var got int64
	for i := 1; i <= 40; i++ {
		got = r.add(t0.Add(time.Duration(i)*250*time.Millisecond), int64(i)*2_500_000)
	}
	if got < 9_900_000 || got > 10_100_000 {
		t.Fatalf("rate = %d, want about 10 MB/s", got)
	}
	if len(r.at) > 16 {
		t.Fatalf("window kept %d samples", len(r.at))
	}
}

// A GPU setup that only lacks a model (a new automatic default, or the
// package model) fetches it and stays on the GPU; it must not take the missing
// file for a GPU that isn't set up.
func TestMissingModelKeepsTheGPU(t *testing.T) {
	fakeNVIDIA(t)
	dir := t.TempDir()
	s := Store{Dir: dir}
	cuda := PackByID("cuda")
	for _, a := range cuda.Artifacts {
		d := s.artifactDir(cuda.ID, a)
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, ".sha256"), []byte(a.SHA256), 0o644)
	}
	markCPUSetup(s) // the CPU was set up before the GPU came
	gm := ModelByID("yolox-m")
	_ = os.MkdirAll(filepath.Dir(s.ModelPath(gm)), 0o755)
	_ = os.WriteFile(s.ModelPath(gm)+".sha256", []byte(gm.Artifact.SHA256), 0o644)

	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 1024))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	defer srv.Close()
	defer close(stop)
	stall := func(a *Artifact) func() {
		saved := *a
		a.URL = srv.URL + "/" + a.Name + ".onnx"
		return func() { *a = saved }
	}
	aio := ModelByID("dfine-x-obj365")
	defer stall(&aio.Artifact)()
	pm := &PackageModels[0]
	defer stall(&pm.Artifact)()

	m := NewManager(dir, "/nonexistent/apdetect", 3, slog.Default())
	defer stopManager(m)

	// Automatic: the all-in-one model, which finds parcels itself.
	m.Configure(&Settings{Device: "auto", Model: "auto", Packages: true}, false)
	st := m.Status()
	if st.State != "installing" || st.Upgrade != nil || st.Install.Total != aio.Artifact.Size {
		t.Fatalf("state %s, upgrade %+v, %d bytes: want the all-in-one model downloading for the GPU", st.State, st.Upgrade, st.Install.Total)
	}

	// YOLOX-M with parcels checked separately: just the package model.
	m.Configure(&Settings{Device: "auto", Model: "yolox-m", Packages: true, PackageMode: "separate"}, false)
	st = m.Status()
	if st.State != "installing" || st.Upgrade != nil || st.Install.Total != pm.Artifact.Size {
		t.Fatalf("state %s, %d bytes: want just the package model (%d)", st.State, st.Install.Total, pm.Artifact.Size)
	}
	// The all-in-one model with "separate" asked for still needs it too.
	if got := packageModel(Settings{Packages: true, PackageMode: "separate"}, Device{Kind: "gpu"}, aio); got == nil {
		t.Fatal("separate parcel check ignored with an all-in-one model")
	}
	if got := packageModel(Settings{Packages: true}, Device{Kind: "gpu"}, aio); got != nil {
		t.Fatal("an all-in-one model got a separate package model anyway")
	}
}

// stopManager turns recognition off and waits until a download has stopped
// (it writes into the test's temp dir).
func stopManager(m *Manager) {
	m.Configure(nil, false)
	m.mu.Lock()
	done := m.installed
	m.mu.Unlock()
	if done != nil {
		<-done
	}
}
