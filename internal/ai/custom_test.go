package ai

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeProbe is an apdetect stand-in whose -probe answers with these outputs.
func fakeProbe(t *testing.T, outputs string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "apdetect")
	script := "#!/bin/sh\necho '{\"ready\":true,\"input\":[1,3,640,640],\"size\":640,\"outputs\":" + outputs + ",\"arch\":\"\",\"classes\":0}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func upload(t *testing.T, m *Manager, up CustomUpload) (ModelView, error) {
	t.Helper()
	p, err := m.UploadPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(p, []byte("onnx"), 0o644)
	up.Path, up.Size, up.SHA256 = p, 4, "abc"
	return m.AddCustomModel(context.Background(), up)
}

func TestAddCustomModels(t *testing.T) {
	dir := t.TempDir()
	markCPUSetup(Store{Dir: dir})

	// An Objects365 DETR model: recognised as all-in-one by its outputs.
	m := NewManager(dir, fakeProbe(t, `[{"name":"logits","shape":[1,300,366]},{"name":"pred_boxes","shape":[1,300,4]}]`), 2, slog.Default())
	v, err := upload(t, m, CustomUpload{Name: "My D-FINE"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v.ID, "custom-") || v.Arch != "detr" || v.Labels != LabelsObjects365 || !v.AllInOne || !v.Custom {
		t.Fatalf("added %+v", v)
	}
	if !m.HasModel(v.ID) || !m.store.ModelInstalled(m.modelByID(v.ID)) {
		t.Fatal("added model not found or not installed")
	}

	// A model with 3 classes of its own needs their names...
	m.bin = fakeProbe(t, `[{"name":"output","shape":[1,8400,8]}]`)
	if _, err := upload(t, m, CustomUpload{Name: "Door"}); err == nil || !strings.Contains(err.Error(), "3 classes") {
		t.Fatalf("unknown classes accepted: %v", err)
	}
	// ...the right number of them...
	if _, err := upload(t, m, CustomUpload{Name: "Door", Classes: "person\nparcel"}); err == nil {
		t.Fatal("2 names for 3 classes accepted")
	}
	// ...and then works, with its parcels found and its own names written for
	// the worker.
	own, err := upload(t, m, CustomUpload{Name: "Door", Classes: "Person\nParcel\nLicense plate\n"})
	if err != nil {
		t.Fatal(err)
	}
	if own.Arch != "yolox" || own.Classes != 3 || !own.AllInOne {
		t.Fatalf("own classes: %+v", own)
	}
	if arg := m.store.ClassesArg(m.modelByID(own.ID)); !strings.HasSuffix(arg, ".classes.txt") {
		t.Fatalf("classes arg %q", arg)
	}

	// Something that isn't a detector is refused.
	m.bin = fakeProbe(t, `[{"name":"probs","shape":[1,1000]}]`)
	if _, err := upload(t, m, CustomUpload{}); err == nil {
		t.Fatal("a classifier was accepted")
	}

	// Removing: not while in use.
	m.mu.Lock()
	m.want = &Settings{Model: v.ID}
	m.mu.Unlock()
	if err := m.RemoveCustomModel(v.ID); err == nil {
		t.Fatal("removed a model in use")
	}
	if err := m.RemoveCustomModel(own.ID); err != nil || m.HasModel(own.ID) {
		t.Fatalf("remove: %v", err)
	}
	if n := len(m.store.CustomModels()); n != 1 {
		t.Fatalf("%d models left, want 1", n)
	}
}
