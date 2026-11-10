package ai

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// The apdetect worker speaks a small protocol over its stdin/stdout:
//
//   - On start it prints one JSON Hello line: ready (with the provider it
//     actually got and the model input size) or an error, after which it exits.
//   - Each request is a binary frame: "APDF" (for the main model) or "APDP"
//     (for the package model), a uint32 id, uint16 width and height, then
//     width*height*3 bytes of packed RGB.
//   - Each reply is one JSON Result line with the same id.
//
// It exits when stdin closes.

// Hello is the worker's first line.
type Hello struct {
	Ready     bool     `json:"ready"`
	Error     string   `json:"error,omitempty"`
	Version   string   `json:"version,omitempty"`   // ONNX Runtime version
	Provider  string   `json:"provider,omitempty"`  // cpu | cuda
	Providers []string `json:"providers,omitempty"` // what the library offers
	Input     int      `json:"input,omitempty"`     // model input side in pixels
	WarmupMs  float64  `json:"warmupMs,omitempty"`
	Packages  bool     `json:"packages,omitempty"` // the separate package model is loaded too
	Arch      string   `json:"arch,omitempty"`     // yolox | detr
	Classes   int      `json:"classes,omitempty"`  // classes the main model knows
	Combined  bool     `json:"combined,omitempty"` // the main model finds parcels itself
}

// Result is the reply to one frame.
type Result struct {
	ID    uint32  `json:"id"`
	Ms    float64 `json:"ms"`
	Boxes []Box   `json:"boxes"`
	Error string  `json:"error,omitempty"`
}

var (
	frameMagic   = [4]byte{'A', 'P', 'D', 'F'}
	packageMagic = [4]byte{'A', 'P', 'D', 'P'}
)

// Frame kinds: which model a frame is for.
const (
	KindObjects  = 0 // the main model
	KindPackages = 1 // the package model
)

// maxFrameSide bounds a frame so a corrupt header can't ask for gigabytes.
const maxFrameSide = 4096

// WriteFrame sends one frame request for the main model.
func WriteFrame(w io.Writer, id uint32, width, height int, rgb []byte) error {
	return WriteFrameKind(w, KindObjects, id, width, height, rgb)
}

// WriteFrameKind sends one frame request for a model.
func WriteFrameKind(w io.Writer, kind int, id uint32, width, height int, rgb []byte) error {
	if width <= 0 || height <= 0 || width > maxFrameSide || height > maxFrameSide || len(rgb) != width*height*3 {
		return fmt.Errorf("bad frame %dx%d (%d bytes)", width, height, len(rgb))
	}
	var hdr [12]byte
	copy(hdr[:4], frameMagic[:])
	if kind == KindPackages {
		copy(hdr[:4], packageMagic[:])
	}
	binary.LittleEndian.PutUint32(hdr[4:], id)
	binary.LittleEndian.PutUint16(hdr[8:], uint16(width))
	binary.LittleEndian.PutUint16(hdr[10:], uint16(height))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(rgb)
	return err
}

// ReadFrame reads one frame request, reusing buf when it is large enough.
func ReadFrame(r *bufio.Reader, buf []byte) (id uint32, width, height int, rgb []byte, err error) {
	_, id, width, height, rgb, err = ReadFrameKind(r, buf)
	return
}

// ReadFrameKind reads one frame request and which model it is for.
func ReadFrameKind(r *bufio.Reader, buf []byte) (kind int, id uint32, width, height int, rgb []byte, err error) {
	var hdr [12]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return
	}
	switch [4]byte(hdr[:4]) {
	case frameMagic:
		kind = KindObjects
	case packageMagic:
		kind = KindPackages
	default:
		err = errors.New("bad frame header")
		return
	}
	id = binary.LittleEndian.Uint32(hdr[4:])
	width = int(binary.LittleEndian.Uint16(hdr[8:]))
	height = int(binary.LittleEndian.Uint16(hdr[10:]))
	if width <= 0 || height <= 0 || width > maxFrameSide || height > maxFrameSide {
		err = fmt.Errorf("bad frame size %dx%d", width, height)
		return
	}
	n := width * height * 3
	if cap(buf) < n {
		buf = make([]byte, n)
	}
	rgb = buf[:n]
	_, err = io.ReadFull(r, rgb)
	return
}

// Probe is what apdetect -probe says about a model.
type Probe struct {
	Ready   bool          `json:"ready"`
	Error   string        `json:"error,omitempty"`
	Input   []int64       `json:"input,omitempty"`   // input shape (-1 = any)
	Size    int           `json:"size,omitempty"`    // side it was run at
	Outputs []ProbeOutput `json:"outputs,omitempty"` // as run on a blank picture
	Arch    string        `json:"arch,omitempty"`    // yolox | detr | "" (not recognised)
	Classes int           `json:"classes,omitempty"` // classes the outputs carry
}

// ProbeOutput is one model output.
type ProbeOutput struct {
	Name  string  `json:"name"`
	Shape []int64 `json:"shape"`
}

// GuessFormat tells a model's format by its outputs: YOLOX has one
// [1, N, 5+classes] output; a DETR model has boxes [1, Q, 4] and logits
// [1, Q, classes].
func GuessFormat(outs []ProbeOutput) (arch string, classes int) {
	if len(outs) == 1 && len(outs[0].Shape) == 3 && outs[0].Shape[2] >= 6 {
		return "yolox", int(outs[0].Shape[2]) - 5
	}
	var boxes, logits *ProbeOutput
	for i := range outs {
		o := &outs[i]
		if len(o.Shape) != 3 {
			continue
		}
		switch {
		case o.Shape[2] == 4 && boxes == nil:
			boxes = o
		case logits == nil:
			logits = o
		}
	}
	if boxes != nil && logits != nil && boxes.Shape[1] == logits.Shape[1] && logits.Shape[2] > 0 {
		return "detr", int(logits.Shape[2])
	}
	return "", 0
}
