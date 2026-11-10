package ai

import (
	"context"
	"image"
	"image/draw"
	_ "image/jpeg"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestWorkerIntegration runs the real apdetect worker against a downloaded ONNX
// Runtime and model. It is skipped unless these are set:
//
//	APERTURE_TEST_APDETECT  path to the apdetect binary
//	APERTURE_TEST_ORT_LIB   path to libonnxruntime.so
//	APERTURE_TEST_MODEL     path to a YOLOX .onnx model
//	APERTURE_TEST_IMAGE     a JPEG to run (optional)
//	APERTURE_TEST_PROVIDER  cpu (default) or cuda
//	APERTURE_TEST_LIBDIRS   colon-separated CUDA library folders (for cuda)
//	APERTURE_TEST_RUNS      runs to time after the first three (benchmark)
//	APERTURE_TEST_PACKAGES  the package model (.onnx), to run it too
//	APERTURE_TEST_ARCH      the main model format (yolox, detr) and
//	APERTURE_TEST_CLASSES   its class list (coco80, obj365, a file)
func TestWorkerIntegration(t *testing.T) {
	bin, lib, model := os.Getenv("APERTURE_TEST_APDETECT"), os.Getenv("APERTURE_TEST_ORT_LIB"), os.Getenv("APERTURE_TEST_MODEL")
	if bin == "" || lib == "" || model == "" {
		t.Skip("set APERTURE_TEST_APDETECT, APERTURE_TEST_ORT_LIB and APERTURE_TEST_MODEL to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	provider := os.Getenv("APERTURE_TEST_PROVIDER")
	if provider == "" {
		provider = "cpu"
	}
	var libDirs []string
	if d := os.Getenv("APERTURE_TEST_LIBDIRS"); d != "" {
		libDirs = strings.Split(d, ":")
	}
	w, err := StartWorker(ctx, WorkerConfig{Bin: bin, Lib: lib, Model: model, Provider: provider, Threads: 3, LibDirs: libDirs, MinScore: minScore(), Packages: os.Getenv("APERTURE_TEST_PACKAGES"),
		Arch: os.Getenv("APERTURE_TEST_ARCH"), Classes: os.Getenv("APERTURE_TEST_CLASSES")}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	h := w.Hello()
	t.Logf("ready: ORT %s, %s, input %d, warm-up %.1f ms, providers %v", h.Version, h.Provider, h.Input, h.WarmupMs, h.Providers)

	rgb, iw, ih := make([]byte, 320*180*3), 320, 180
	if p := os.Getenv("APERTURE_TEST_IMAGE"); p != "" {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		b := img.Bounds()
		rgba := image.NewRGBA(b)
		draw.Draw(rgba, b, img, b.Min, draw.Src)
		iw, ih = b.Dx(), b.Dy()
		rgb = make([]byte, iw*ih*3)
		for i := 0; i < iw*ih; i++ {
			copy(rgb[i*3:i*3+3], rgba.Pix[i*4:i*4+3])
		}
	}
	for i := 0; i < 3; i++ {
		res, err := w.Detect(ctx, rgb, iw, ih)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("run %d: %.1f ms, %d boxes", i, res.Ms, len(res.Boxes))
		if i == 2 {
			for _, b := range res.Boxes {
				t.Logf("  %-12s %.2f  x=%.3f y=%.3f w=%.3f h=%.3f", Label(b.Class), b.Score, b.X, b.Y, b.W, b.H)
			}
		}
	}
	if h.Packages {
		for i := 0; i < 3; i++ {
			res, err := w.DetectKind(ctx, KindPackages, rgb, iw, ih)
			if err != nil {
				t.Fatal(err)
			}
			if i == 2 {
				t.Logf("packages: %.1f ms", res.Ms)
				for _, b := range res.Boxes {
					t.Logf("  %-12s %.2f  x=%.3f y=%.3f w=%.3f h=%.3f", Label(b.Class), b.Score, b.X, b.Y, b.W, b.H)
				}
			}
		}
	}
	runs, _ := strconv.Atoi(os.Getenv("APERTURE_TEST_RUNS"))
	if runs <= 0 {
		return
	}
	// Timed as nvrd sees it (frame transfer included) and as the worker
	// reports it (letterbox + inference).
	var worker float64
	start := time.Now()
	for i := 0; i < runs; i++ {
		res, err := w.Detect(ctx, rgb, iw, ih)
		if err != nil {
			t.Fatal(err)
		}
		worker += res.Ms
	}
	wall := time.Since(start)
	t.Logf("BENCH %dx%d %s: %.1f ms per frame in the worker, %.1f ms round trip, %.0f frames/s", iw, ih, provider,
		worker/float64(runs), float64(wall.Microseconds())/1000/float64(runs), float64(runs)/wall.Seconds())
}

func minScore() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("APERTURE_TEST_SCORE"), 64); err == nil {
		return v
	}
	return 0.3
}
