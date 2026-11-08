package record

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// Per-camera recording quality. Live view always gets what the camera sends; a
// camera with a RecordHeight, RecordFPS or RecordKbps records a re-encoded copy. With
// none of them set the recorder stream-copies exactly as it always has, at near-zero
// CPU, so nothing changes for a camera nobody opted in.
//
// Re-encoding runs in software and costs about a core per camera, so it can fail in
// ways stream copy cannot. A camera whose re-encoder keeps dying falls back to the
// original stream: a quality preference must never cost the footage itself.
const (
	reencodeCRF        = "26" // automatic bitrate: a constant-quality target
	reencodePreset     = "veryfast"
	reencodeKeyframes  = "expr:gte(t,n_forced*2)" // seekable: a keyframe every 2 s, whatever the framerate
	reencodeMaxQuick   = 3                        // consecutive quick exits before falling back
	reencodeQuickUnder = 30 * time.Second         // an exit this soon counts as a failure to run
	// framerateSlack shortens the gap a framerate cap asks for between frames, so
	// timing jitter does not drop an extra frame: without it a 10 fps cap on a 20 fps
	// camera lands near 8 fps.
	framerateSlack = 0.85
)

// wantsReencode reports whether a camera records something other than the stream.
func wantsReencode(c store.Camera) bool {
	return c.RecordHeight > 0 || c.RecordFPS > 0 || c.RecordKbps > 0
}

// videoArgs returns the ffmpeg video options for a recording: stream copy unless
// reencode is set. Sizes and framerates are ceilings, never targets: a camera below
// the chosen height keeps its size, and a framerate cap only ever drops frames,
// which is why it is a select filter and not fps=, which duplicates frames to reach
// its target.
func videoArgs(c store.Camera, reencode bool) []string {
	if !reencode {
		return []string{"-c:v", "copy"}
	}
	var filters []string
	if c.RecordFPS > 0 {
		gap := strconv.FormatFloat(framerateSlack/float64(c.RecordFPS), 'f', 4, 64)
		filters = append(filters, fmt.Sprintf("select='isnan(prev_selected_t)+gte(t-prev_selected_t,%s)'", gap))
	}
	if c.RecordHeight > 0 {
		filters = append(filters, fmt.Sprintf("scale=-2:'min(%d,ih)'", c.RecordHeight))
	}
	var args []string
	if len(filters) > 0 {
		args = append(args, "-vf", strings.Join(filters, ","))
	}
	args = append(args,
		"-fps_mode", "vfr", // keep the frames that survive, without padding back to a rate
		"-c:v", "libx264", "-preset", reencodePreset,
		"-pix_fmt", "yuv420p", // cameras often send 10-bit HEVC; browsers need 8-bit H.264
		"-force_key_frames", reencodeKeyframes,
	)
	if c.RecordKbps > 0 {
		k := strconv.Itoa(c.RecordKbps) + "k"
		return append(args, "-b:v", k, "-maxrate", k, "-bufsize", strconv.Itoa(c.RecordKbps*2)+"k")
	}
	return append(args, "-crf", reencodeCRF)
}

// keepReencoding counts a re-encoder's exit and reports whether to try again. quick
// is the run of consecutive quick exits, carried between calls.
func keepReencoding(ran time.Duration, quick *int) bool {
	if ran >= reencodeQuickUnder {
		*quick = 0
		return true
	}
	*quick++
	return *quick < reencodeMaxQuick
}
