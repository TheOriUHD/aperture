package detect

import (
	"fmt"
	"log/slog"
)

// newONNX builds the embedded local-model backend. In-process ONNX inference
// requires onnxruntime_go, which is CGO-only and pulls a native ONNX Runtime
// library into the image — abandoning Aperture's pure-Go, CGO-free single
// binary. To keep that build lean, the default image ships no in-process model:
// this reports unavailable and the engine cleanly no-ops (detection stays off).
//
// For LOCAL inference without the CGO cost, run a detector sidecar (e.g.
// CodeProject.AI) on the same host and use the "bridge" backend — see
// docs/detection.md. An embedded ONNX build is intended to live behind a build
// tag / separate image so the default binary stays pure-Go.
func newONNX(c Config, dataDir string, log *slog.Logger) (Backend, error) {
	return nil, fmt.Errorf("%w: embedded model not built into this (pure-Go) image — run a local detector sidecar and use the External server backend (see docs/detection.md)", errUnavailable)
}
