// Command apdetect is Aperture's object-detection worker. nvrd starts it with a
// downloaded ONNX Runtime library and model, sends it video frames over stdin
// and reads detections back from stdout (see internal/ai/proto.go). It runs as
// its own process so that a fault in native code or a GPU driver can never take
// recording down with it; nvrd restarts it if it dies.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/TheOriUHD/aperture/internal/ai"
	"github.com/TheOriUHD/aperture/internal/ai/ort"
)

func main() {
	lib := flag.String("lib", "", "path to libonnxruntime.so")
	model := flag.String("model", "", "path to the .onnx model")
	provider := flag.String("provider", ort.ProviderCPU, "execution provider: cpu or cuda")
	device := flag.Int("device", 0, "GPU index (cuda)")
	threads := flag.Int("threads", 2, "CPU threads per inference (cpu)")
	minScore := flag.Float64("score", 0.25, "lowest score reported")
	packages := flag.String("packages", "", "path to the package model (D-FINE, Objects365), optional")
	packageScore := flag.Float64("package-score", 0.12, "lowest package model score reported (parcels score low; nvrd decides)")
	flag.Parse()

	out := bufio.NewWriterSize(os.Stdout, 64<<10)
	enc := json.NewEncoder(out)
	fail := func(err error) {
		_ = enc.Encode(ai.Hello{Error: err.Error()})
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
	sess, err := rt.NewSession(*model, ort.SessionOptions{Provider: *provider, Device: *device, Threads: *threads})
	if err != nil {
		fail(err)
	}
	size := 640
	if s := sess.InputShape; len(s) == 4 && s[2] > 0 && s[2] == s[3] {
		size = int(s[2])
	}
	input := make([]float32, 3*size*size)
	shape := []int64{1, 3, int64(size), int64(size)}

	// Warm up: the first run allocates and (on a GPU) picks kernels.
	for i := range input {
		input[i] = 114
	}
	t0 := time.Now()
	if _, _, err := sess.Run(input, shape); err != nil {
		fail(fmt.Errorf("first inference: %w", err))
	}

	// The package model, when asked for: same runtime and device.
	var pkg *packageModel
	if *packages != "" {
		ps, err := rt.NewSession(*packages, ort.SessionOptions{Provider: *provider, Device: *device, Threads: *threads})
		if err != nil {
			fail(fmt.Errorf("package model: %w", err))
		}
		pkg = newPackageModel(ps)
		if _, err := pkg.run(make([]byte, 64*36*3), 64, 36, 0.99); err != nil {
			fail(fmt.Errorf("package model, first inference: %w", err))
		}
	}
	_ = enc.Encode(ai.Hello{
		Ready: true, Version: rt.Version(), Provider: *provider, Providers: rt.Providers(),
		Input: size, WarmupMs: ms(time.Since(t0)), Packages: pkg != nil,
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
		if kind == ai.KindPackages {
			if pkg == nil {
				res.Error = "no package model loaded"
			} else if res.Boxes, err = pkg.run(rgb, w, h, float32(*packageScore)); err != nil {
				res.Error = err.Error()
			}
			res.Ms = ms(time.Since(start))
			if res.Boxes == nil {
				res.Boxes = []ai.Box{}
			}
			if enc.Encode(res) != nil || out.Flush() != nil {
				return
			}
			continue
		}
		tiles := ai.Tiles(w, h)
		var boxes []ai.Box
		for _, t := range tiles {
			scale := ai.LetterboxRect(rgb, w, h, t[0], 0, t[1], h, size, input)
			raw, dims, err := sess.Run(input, shape)
			if err != nil {
				res.Error = err.Error()
				break
			}
			for _, b := range ai.DecodeYOLOX(raw, dims, size, scale, t[1], h, float32(*minScore)) {
				// Back from tile to frame coordinates.
				b.X = (float32(t[0]) + b.X*float32(t[1])) / float32(w)
				b.W = b.W * float32(t[1]) / float32(w)
				boxes = append(boxes, b)
			}
		}
		if len(tiles) > 1 {
			boxes = ai.NMS(boxes, 0.45)
		}
		res.Boxes = boxes
		res.Ms = ms(time.Since(start))
		if res.Boxes == nil {
			res.Boxes = []ai.Box{}
		}
		if err := enc.Encode(res); err != nil {
			return
		}
		if err := out.Flush(); err != nil {
			return
		}
	}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// packageModel runs D-FINE (see internal/ai/dfine.go).
type packageModel struct {
	s      *ort.Session
	size   int
	input  []float32
	shape  []int64
	logits int // output indexes
	boxes  int
}

func newPackageModel(s *ort.Session) *packageModel {
	const size = 640 // as the model was trained (its input is dynamic)
	p := &packageModel{s: s, size: size, input: make([]float32, 3*size*size), shape: []int64{1, 3, size, size}, boxes: 1}
	for i, name := range s.Outputs {
		switch name {
		case "logits":
			p.logits = i
		case "pred_boxes":
			p.boxes = i
		}
	}
	return p
}

// run finds people and packages in a frame, tiling very wide ones.
func (p *packageModel) run(rgb []byte, w, h int, minScore float32) ([]ai.Box, error) {
	tiles := ai.Tiles(w, h)
	var boxes []ai.Box
	for _, t := range tiles {
		ai.Stretch(rgb, w, h, t[0], 0, t[1], h, p.size, p.input)
		outs, err := p.s.RunAll(p.input, p.shape)
		if err != nil {
			return nil, err
		}
		if len(outs) < 2 || len(outs[p.logits].Shape) != 3 {
			return nil, fmt.Errorf("unexpected package model outputs %v", p.s.Outputs)
		}
		lg := outs[p.logits]
		for _, b := range ai.DecodeDFINE(lg.Data, outs[p.boxes].Data, int(lg.Shape[1]), int(lg.Shape[2]), minScore) {
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
