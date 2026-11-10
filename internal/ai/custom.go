package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Models the operator adds live in <store>/custom: <id>.onnx with <id>.json
// beside it (what Aperture found out about it, and the name it was given),
// plus <id>.classes.txt when it has its own class list.

const customPrefix = "custom-"

type customMeta struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Arch    string   `json:"arch"`
	Labels  string   `json:"labels"` // a label set, or "custom" (Classes)
	Classes []string `json:"classes,omitempty"`
	Input   int      `json:"input"`
	Size    int64    `json:"size"`
	SHA256  string   `json:"sha256"`
	Added   int64    `json:"added"`
}

func (s Store) customDir() string { return filepath.Join(s.Dir, "custom") }

// CustomModels lists the models the operator added, oldest first.
func (s Store) CustomModels() []*Model {
	paths, _ := filepath.Glob(filepath.Join(s.customDir(), customPrefix+"*.json"))
	var metas []customMeta
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cm customMeta
		if json.Unmarshal(b, &cm) == nil && cm.ID != "" {
			metas = append(metas, cm)
		}
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Added < metas[j].Added })
	out := make([]*Model, 0, len(metas))
	for _, cm := range metas {
		out = append(out, cm.model())
	}
	return out
}

// CustomModel finds an added model by id.
func (s Store) CustomModel(id string) *Model {
	if !strings.HasPrefix(id, customPrefix) {
		return nil
	}
	for _, m := range s.CustomModels() {
		if m.ID == id {
			return m
		}
	}
	return nil
}

func (cm customMeta) model() *Model {
	m := &Model{ID: cm.ID, Name: cm.Name, Input: cm.Input, Arch: cm.Arch, Labels: cm.Labels, Classes: cm.Classes,
		Custom: true, Artifact: Artifact{Name: cm.ID, Kind: "file", SHA256: cm.SHA256, Size: cm.Size}}
	m.AllInOne = FindsPackages(VocabMap(m.LabelSetOf(), m.ClassList()))
	what := "COCO classes"
	switch m.LabelSetOf() {
	case LabelsObjects365:
		what = "Objects365 classes"
	case "custom":
		what = "its own classes"
	}
	m.Detail = fmt.Sprintf("Your model · %s · %d %s", strings.ToUpper(m.ArchOf()), len(m.ClassList()), what)
	if m.AllInOne {
		m.Detail += " · finds parcels"
	}
	return m
}

// ClassesArg is apdetect's -classes value for a model: its label set, or a
// file with its own class names.
func (s Store) ClassesArg(m *Model) string {
	if m.LabelSetOf() != "custom" {
		return m.LabelSetOf()
	}
	p := filepath.Join(s.customDir(), m.ID+".classes.txt")
	if _, err := os.Stat(p); err != nil {
		_ = os.WriteFile(p, []byte(strings.Join(m.Classes, "\n")+"\n"), 0o644)
	}
	return p
}

// UploadPath is a fresh temporary path to receive an uploaded model.
func (s Store) UploadPath() (string, error) {
	if err := os.MkdirAll(filepath.Join(s.customDir(), "tmp"), 0o755); err != nil {
		return "", err
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	return filepath.Join(s.customDir(), "tmp", hex.EncodeToString(b[:])+".onnx"), nil
}

// UploadPath is where an upload to this manager goes.
func (m *Manager) UploadPath() (string, error) { return m.store.UploadPath() }

// CustomUpload is what came with an uploaded model.
type CustomUpload struct {
	Path    string // where it was received (UploadPath)
	SHA256  string
	Size    int64
	Name    string
	Arch    string // "" = as found
	Classes string // class names, one per line ("" = as found)
}

// Probe describes a model: it runs apdetect -probe with whichever ONNX
// Runtime is installed.
func (m *Manager) Probe(ctx context.Context, path string) (Probe, error) {
	var lib string
	var dirs []string
	for _, id := range []string{"cpu", "cuda"} {
		if p := PackByID(id); p != nil && m.store.PackInstalled(p) {
			lib, dirs = m.store.LibPath(p), m.store.LibDirs(p)
			break
		}
	}
	if lib == "" {
		return Probe{}, errors.New("set up object recognition first: checking a model needs its runtime")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.bin, "-probe", "-lib", lib, "-model", path)
	cmd.Env = os.Environ()
	if len(dirs) > 0 {
		cmd.Env = append(cmd.Env, "LD_LIBRARY_PATH="+strings.Join(dirs, ":"))
	}
	out, _ := cmd.Output()
	var p Probe
	if err := json.Unmarshal(firstLineBytes(out), &p); err != nil {
		return Probe{}, errors.New("couldn't load the model (is it an ONNX file?)")
	}
	if !p.Ready {
		reason := firstLine(p.Error)
		if strings.Contains(reason, "Protobuf parsing failed") || strings.Contains(reason, "No graph") {
			return p, errors.New("this file is not an ONNX model")
		}
		return p, fmt.Errorf("the model can not be loaded: %s", reason)
	}
	return p, nil
}

func firstLineBytes(b []byte) []byte {
	if i := strings.IndexByte(string(b), '\n'); i >= 0 {
		return b[:i]
	}
	return b
}

// AddCustomModel checks an uploaded model and keeps it. Its format and class
// list are worked out from its outputs unless the upload says otherwise.
func (m *Manager) AddCustomModel(ctx context.Context, up CustomUpload) (ModelView, error) {
	defer os.Remove(up.Path)
	p, err := m.Probe(ctx, up.Path)
	if err != nil {
		return ModelView{}, err
	}
	// The format is told by the outputs (see GuessFormat), not taken on trust.
	arch, classes := GuessFormat(p.Outputs)
	if arch == "" || classes == 0 {
		return ModelView{}, fmt.Errorf("Aperture can't run this model: its outputs (%s) are neither YOLOX's (one [1, N, 5+classes]) nor a DETR model's (logits and [1, Q, 4] boxes)", outputsText(p.Outputs))
	}
	if up.Arch != "" && up.Arch != arch {
		return ModelView{}, fmt.Errorf("its outputs are a %s model's, not %s", strings.ToUpper(arch), strings.ToUpper(up.Arch))
	}
	set, names := GuessLabels(classes)
	if txt := strings.TrimSpace(up.Classes); txt != "" {
		names = nil
		for _, l := range strings.Split(strings.ReplaceAll(txt, "\r\n", "\n"), "\n") {
			names = append(names, strings.TrimSpace(l))
		}
		if len(names) != classes {
			return ModelView{}, fmt.Errorf("the model has %d classes, but %d names were given", classes, len(names))
		}
		set = "custom"
	}
	if names == nil {
		return ModelView{}, fmt.Errorf("the model has %d classes: paste their names, one per line, in order", classes)
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	id := customPrefix + hex.EncodeToString(b[:])
	name := strings.TrimSpace(up.Name)
	if name == "" {
		name = "My model"
	}
	cm := customMeta{ID: id, Name: name, Arch: arch, Labels: set, Input: p.Size, Size: up.Size, SHA256: up.SHA256,
		Added: time.Now().UnixMilli()}
	if set == "custom" {
		cm.Classes = names
	}
	mod := cm.model()
	if !FindsPackages(VocabMap(mod.LabelSetOf(), mod.ClassList())) && !knowsAny(VocabMap(mod.LabelSetOf(), mod.ClassList())) {
		return ModelView{}, errors.New("none of its classes match what Aperture reports (people, vehicles, animals, parcels)")
	}
	dir := m.store.customDir()
	if err := os.Rename(up.Path, filepath.Join(dir, id+".onnx")); err != nil {
		return ModelView{}, err
	}
	b2, _ := json.MarshalIndent(cm, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, id+".json"), b2, 0o644); err != nil {
		_ = os.Remove(filepath.Join(dir, id+".onnx"))
		return ModelView{}, err
	}
	m.log.Info("object recognition: model added", "id", id, "name", name, "arch", arch, "classes", classes)
	m.mu.Lock()
	d := m.device
	m.mu.Unlock()
	return viewOf(mod, d, true, false), nil
}

func knowsAny(vocab []int) bool {
	for _, v := range vocab {
		if v >= 0 {
			return true
		}
	}
	return false
}

func outputsText(outs []ProbeOutput) string {
	var parts []string
	for _, o := range outs {
		parts = append(parts, fmt.Sprintf("%s %v", o.Name, o.Shape))
	}
	return strings.Join(parts, ", ")
}

// RemoveCustomModel deletes an added model (not while it is in use).
func (m *Manager) RemoveCustomModel(id string) error {
	if m.store.CustomModel(id) == nil {
		return os.ErrNotExist
	}
	m.mu.Lock()
	inUse := m.want != nil && m.want.Model == id
	m.mu.Unlock()
	if inUse {
		return errors.New("this model is in use: choose another first")
	}
	dir := m.store.customDir()
	for _, ext := range []string{".onnx", ".json", ".classes.txt"} {
		_ = os.Remove(filepath.Join(dir, id+ext))
	}
	return nil
}
