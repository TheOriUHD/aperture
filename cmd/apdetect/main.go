// Command apdetect is Aperture's object-detection worker. nvrd starts it with a
// downloaded ONNX Runtime library and model, sends it video frames over stdin
// and reads detections back from stdout (see internal/ai/proto.go). It runs as
// its own process so that a fault in native code or a GPU driver can never take
// recording down with it; nvrd restarts it if it dies.
//
// With -probe it only loads a model, runs it once and describes its inputs and
// outputs (for checking a model an operator adds).
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TheOriUHD/aperture/internal/ai"
	"github.com/TheOriUHD/aperture/internal/ai/ort"
)

func main() {
	lib := flag.String("lib", "", "path to libonnxruntime.so")
	model := flag.String("model", "", "path to the .onnx model")
	arch := flag.String("arch", "yolox", "model format: yolox or detr")
	classes := flag.String("classes", ai.LabelsCOCO, "class list: coco80, coco91, obj365, or a file with one name per line")
	provider := flag.String("provider", ort.ProviderCPU, "execution provider: cpu or cuda")
	device := flag.Int("device", 0, "GPU index (cuda)")
	threads := flag.Int("threads", 2, "CPU threads per inference (cpu)")
	minScore := flag.Float64("score", 0.25, "lowest score reported")
	packages := flag.String("packages", "", "path to the separate package model (D-FINE, Objects365), optional")
	packageScore := flag.Float64("package-score", 0.12, "lowest package score reported (parcels score low; nvrd decides)")
	probe := flag.Bool("probe", false, "describe the model and exit")
	flag.Parse()

	out := bufio.NewWriterSize(os.Stdout, 64<<10)
	enc := json.NewEncoder(out)
	fail := func(err error) {
		if *probe {
			_ = enc.Encode(ai.Probe{Error: err.Error()})
		} else {
			_ = enc.Encode(ai.Hello{Error: err.Error()})
		}
		_ = out.Flush()
		os.Exit(1)
	}
	if *lib == "" || *model == "" {
		fail(fmt.Errorf("usage: apdetect -lib libonnxruntime.so -model model.onnx"))
	}

	rt, err := ort.Load(*lib)
	if err != nil {
		fail(err)
	}
	if *probe {
		p, err := describe(rt, *model)
		if err != nil {
			fail(err)
		}
		_ = enc.Encode(p)
		_ = out.Flush()
		return
	}

	names, err := classNames(*classes)
	if err != nil {
		fail(err)
	}
	vocab := ai.VocabMap(labelSet(*classes), names)
	opts := ort.SessionOptions{Provider: *provider, Device: *device, Threads: *threads}
	sess, err := rt.NewSession(*model, opts)
	if err != nil {
		fail(err)
	}
	var main detector
	switch *arch {
	case "detr":
		main = newDETR(sess, vocab)
	case "yolox", "":
		main = newYOLOX(sess, vocab)
	default:
		fail(fmt.Errorf("unknown model format %q", *arch))
	}
	// Warm up: the first run allocates and (on a GPU) picks kernels.
	t0 := time.Now()
	if _, err := main.run(make([]byte, 64*36*3), 64, 36, 0.99); err != nil {
		fail(fmt.Errorf("first inference: %w", err))
	}

	// The separate package model, when asked for: same runtime and device.
	var pkg detector
	if *packages != "" {
		ps, err := rt.NewSession(*packages, opts)
		if err != nil {
			fail(fmt.Errorf("package model: %w", err))
		}
		pkg = newDETR(ps, ai.PackageVocab(ai.VocabMap(ai.LabelsObjects365, ai.Objects365)))
		if _, err := pkg.run(make([]byte, 64*36*3), 64, 36, 0.99); err != nil {
			fail(fmt.Errorf("package model, first inference: %w", err))
		}
	}
	_ = enc.Encode(ai.Hello{
		Ready: true, Version: rt.Version(), Provider: *provider, Providers: rt.Providers(),
		Input: main.inputSize(), WarmupMs: ms(time.Since(t0)), Packages: pkg != nil,
		Arch: *arch, Classes: len(names), Combined: ai.FindsPackages(vocab),
	})
	_ = out.Flush()

	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	var buf []byte
	for {
		kind, id, w, h, rgb, err := ai.ReadFrameKind(in, buf)
		if err != nil {
			return // stdin closed: nvrd is done with us
		}
		buf = rgb
		start := time.Now()
		res := ai.Result{ID: id}
		d, score := main, float32(*minScore)
		if kind == ai.KindPackages {
			d, score = pkg, float32(*packageScore)
		}
		if d == nil {
			res.Error = "no package model loaded"
		} else if res.Boxes, err = d.run(rgb, w, h, score); err != nil {
			res.Error = err.Error()
		}
		res.Ms = ms(time.Since(start))
		if res.Boxes == nil {
			res.Boxes = []ai.Box{}
		}
		if enc.Encode(res) != nil || out.Flush() != nil {
			return
		}
	}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// labelSet is the built-in set a -classes value names ("custom" for a file).
func labelSet(v string) string {
	switch v {
	case ai.LabelsCOCO, ai.LabelsCOCO91, ai.LabelsObjects365:
		return v
	}
	return "custom"
}

// classNames is the class list for -classes: a built-in set or a file.
func classNames(v string) ([]string, error) {
	if names := ai.ClassNames(v); names != nil {
		return names, nil
	}
	b, err := os.ReadFile(v)
	if err != nil {
		return nil, fmt.Errorf("class list: %w", err)
	}
	var names []string
	for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		names = append(names, strings.TrimSpace(l))
	}
	for len(names) > 0 && names[len(names)-1] == "" {
		names = names[:len(names)-1]
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("class list %s is empty", v)
	}
	return names, nil
}

// detector is one loaded model.
type detector interface {
	run(rgb []byte, w, h int, minScore float32) ([]ai.Box, error)
	inputSize() int
}

// fixedSide is a model's square input side when it has one, else def.
func fixedSide(s *ort.Session, def int) int {
	if sh := s.InputShape; len(sh) == 4 && sh[2] > 0 && sh[2] == sh[3] {
		return int(sh[2])
	}
	return def
}

// tiled runs one tile at a time over a frame (very wide frames are tiled, see
// ai.Tiles) and maps the boxes back to frame coordinates.
func tiled(w, h int, run func(x0, tw int) ([]ai.Box, error)) ([]ai.Box, error) {
	tiles := ai.Tiles(w, h)
	var boxes []ai.Box
	for _, t := range tiles {
		bs, err := run(t[0], t[1])
		if err != nil {
			return nil, err
		}
		for _, b := range bs {
			b.X = (float32(t[0]) + b.X*float32(t[1])) / float32(w)
			b.W = b.W * float32(t[1]) / float32(w)
			boxes = append(boxes, b)
		}
	}
	if len(tiles) > 1 {
		boxes = ai.NMS(boxes, 0.45)
	}
	return boxes, nil
}

// yolox runs a YOLOX model (letterboxed BGR input, one [1,N,5+C] output).
type yolox struct {
	s     *ort.Session
	size  int
	input []float32
	shape []int64
	vocab []int
}

func newYOLOX(s *ort.Session, vocab []int) *yolox {
	size := fixedSide(s, 640)
	return &yolox{s: s, size: size, input: make([]float32, 3*size*size), shape: []int64{1, 3, int64(size), int64(size)}, vocab: vocab}
}

func (m *yolox) inputSize() int { return m.size }

func (m *yolox) run(rgb []byte, w, h int, minScore float32) ([]ai.Box, error) {
	return tiled(w, h, func(x0, tw int) ([]ai.Box, error) {
		scale := ai.LetterboxRect(rgb, w, h, x0, 0, tw, h, m.size, m.input)
		raw, dims, err := m.s.Run(m.input, m.shape)
		if err != nil {
			return nil, err
		}
		var out []ai.Box
		for _, b := range ai.DecodeYOLOX(raw, dims, m.size, scale, tw, h, minScore) {
			if b.Class < 0 || b.Class >= len(m.vocab) || m.vocab[b.Class] < 0 {
				continue
			}
			b.Class = m.vocab[b.Class]
			out = append(out, b)
		}
		return out, nil
	})
}

// detr runs a DETR-family model (stretched RGB input; logits and boxes out).
type detr struct {
	s      *ort.Session
	size   int
	input  []float32
	shape  []int64
	logits int // output indexes
	boxes  int
	vocab  []int
}

func newDETR(s *ort.Session, vocab []int) *detr {
	size := fixedSide(s, 640) // a dynamic input runs at the 640 they're trained at
	d := &detr{s: s, size: size, input: make([]float32, 3*size*size), shape: []int64{1, 3, int64(size), int64(size)}, boxes: 1, vocab: vocab}
	for i, name := range s.Outputs {
		switch name {
		case "logits", "pred_logits", "scores":
			d.logits = i
		case "pred_boxes", "boxes":
			d.boxes = i
		}
	}
	if d.logits == d.boxes {
		d.logits, d.boxes = 0, 1
	}
	return d
}

func (m *detr) inputSize() int { return m.size }

func (m *detr) run(rgb []byte, w, h int, minScore float32) ([]ai.Box, error) {
	return tiled(w, h, func(x0, tw int) ([]ai.Box, error) {
		ai.Stretch(rgb, w, h, x0, 0, tw, h, m.size, m.input)
		outs, err := m.s.RunAll(m.input, m.shape)
		if err != nil {
			return nil, err
		}
		if len(outs) < 2 || len(outs[m.logits].Shape) != 3 || len(outs[m.boxes].Shape) != 3 {
			return nil, fmt.Errorf("unexpected outputs %v for a DETR model", m.s.Outputs)
		}
		lg := outs[m.logits]
		return ai.DecodeDETR(lg.Data, outs[m.boxes].Data, int(lg.Shape[1]), int(lg.Shape[2]), m.vocab, minScore), nil
	})
}

// describe loads a model on the CPU, runs it once on a blank picture and
// reports its input, its outputs and what they look like.
func describe(rt *ort.Runtime, path string) (ai.Probe, error) {
	s, err := rt.NewSession(path, ort.SessionOptions{Provider: ort.ProviderCPU, Threads: 2})
	if err != nil {
		return ai.Probe{}, err
	}
	defer s.Close()
	size := fixedSide(s, 640)
	if sh := s.InputShape; len(sh) != 4 || (sh[1] > 0 && sh[1] != 3) {
		return ai.Probe{}, fmt.Errorf("the model's input %v is not one RGB picture (1×3×H×W)", sh)
	}
	outs, err := s.RunAll(make([]float32, 3*size*size), []int64{1, 3, int64(size), int64(size)})
	if err != nil {
		return ai.Probe{}, fmt.Errorf("a test run failed: %w", err)
	}
	p := ai.Probe{Ready: true, Input: s.InputShape, Size: size}
	for i, o := range outs {
		p.Outputs = append(p.Outputs, ai.ProbeOutput{Name: s.Outputs[i], Shape: o.Shape})
	}
	p.Arch, p.Classes = ai.GuessFormat(p.Outputs)
	return p, nil
}
