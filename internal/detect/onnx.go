package detect

import (
	"fmt"
	"log/slog"
)

// newONNX builds the embedded local-model backend. Phase 1 ships the plumbing
// but no bundled runtime, so this reports unavailable and the engine cleanly
// falls back to doing nothing (detection stays off) rather than crashing.
// Phase 2 replaces this with an onnxruntime-backed YOLO detector.
func newONNX(c Config, dataDir string, log *slog.Logger) (Backend, error) {
	return nil, fmt.Errorf("%w: embedded onnx model not built into this image (use the bridge backend, or the onnx build)", errUnavailable)
}
