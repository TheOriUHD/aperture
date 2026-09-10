package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// Serve-time repair of segments whose audio track is declared but empty.
//
// Recording is stream-copy and must stay that way, so whatever a camera emits is what
// lands on disk — including defects. Between 2026-08-20 21:13 and 2026-08-21 05:34 the
// recorder ran ffmpeg 7.1.5 with "-c copy", which announces the AAC track in the PMT and
// then writes ZERO packets to it. ffprobe still prints "aac", so every server-side check
// looked healthy while the web player showed a black rectangle: hls.js reads the PMT,
// expects an audio track, and will not emit its init segment until it has parameters for
// every declared track. The recorder now re-encodes audio, so the broken set is bounded
// and shrinks with retention.
//
// The repair SUBSTITUTES A SILENT TRACK rather than dropping the dead one. That matters
// more than it looks: MediaSource cannot add or remove a SourceBuffer mid-stream, so if
// some segments carry audio and others do not, playback wedges the moment it crosses the
// boundary — and this archive has two such boundaries. Substituting silence keeps the
// track layout identical across the whole archive while preserving real audio wherever it
// exists.
//
// Detection parses the transport stream directly instead of spawning ffprobe: an audio
// PID declared in the PMT that carries no ADTS sync word is dead. That costs microseconds
// over a bounded prefix, so healthy segments keep their zero-copy ServeFile path and never
// pay for this.
//
// Deliberately NOT done here: rewriting the HEVC general_level_idc. The camera declares
// level 5.0 for an 11.3 MPix picture, which is non-conformant, but that same stream played
// correctly under the previous deployment — so it was never the blocker, and declaring a
// higher level can only make a decoder pickier about accepting it.

// repairSem bounds concurrent repairs. This box's first duty is recording; an unbounded
// ffmpeg fan-out from a scrub burst is exactly the kind of load that must never reach the
// recorder. Mirrors frameSem in recordings_handlers.go.
var repairSem = make(chan struct{}, 2)

// repairTimeout caps a single repair, matching the deadline handleFrame uses for its own
// ffmpeg. A repair that cannot finish in this long is not worth stalling a fragment for.
const repairTimeout = 6 * time.Second

// segmentScanBytes is how much of a segment is inspected to decide whether its audio is
// alive. ADTS frames recur every ~24 ms of audio, so a healthy track shows sync words far
// inside this window; the bound keeps a slow network backend from stalling the check.
const segmentScanBytes = 1 << 20

// maxRepairBytes caps how much of a segment is held in memory during a repair. Recorded
// segments are 10s and run ~0.5-7 MB. Anything past this is pathological and is served
// unmodified rather than buffered.
const maxRepairBytes = 64 << 20

const tsPacketLen = 188

// tsAudioState is what a prefix scan can tell us about a segment's audio.
type tsAudioState int

const (
	audioUnknown       tsAudioState = iota // no PMT found in the prefix; leave the segment alone
	audioNone                              // no audio elementary stream is declared
	audioAlive                             // an audio PID is declared and carries ADTS frames
	audioDeclaredEmpty                     // an audio PID is declared but carries nothing decodable
)

// tsScan is what a prefix scan can tell us about a segment.
type tsScan struct {
	audio tsAudioState
	// videoPTS90k is the first video presentation timestamp, in 90 kHz units, and
	// whether one was found. The recorder writes continuous PTS across every segment of
	// a run (there is deliberately no -reset_timestamps), so a segment recorded hours
	// into a run legitimately starts at a very large timestamp. Substituted silence has
	// to begin there too: generated from zero instead, ffmpeg dutifully encodes every
	// second in between — measured at 139 MB and 7.6 s for one 10-second segment, versus
	// 4.9 MB and 148 ms when aligned.
	videoPTS90k int64
	hasVideoPTS bool
}

// scanTS walks MPEG-TS packets to find the PMT, the audio elementary PID and whether it
// carries any ADTS sync word, plus the first video PTS.
func scanTS(buf []byte) tsScan {
	var res tsScan
	pmtPID, audioPID, videoPID := -1, -1, -1
	sawPMT := false

	for i := 0; i+tsPacketLen <= len(buf); i += tsPacketLen {
		if buf[i] != 0x47 {
			continue
		}
		pid := int(buf[i+1]&0x1f)<<8 | int(buf[i+2])
		payloadStart := buf[i+1]&0x40 != 0
		adaptation := (buf[i+3] >> 4) & 0x3
		p := i + 4
		if adaptation == 2 { // adaptation field only, no payload
			continue
		}
		if adaptation == 3 {
			if p >= len(buf) {
				continue
			}
			p += 1 + int(buf[p])
		}
		if p >= i+tsPacketLen || p >= len(buf) {
			continue
		}

		switch {
		case pid == 0 && pmtPID < 0: // PAT
			if payloadStart {
				p += 1 + int(buf[p]) // pointer_field
			}
			if p+12 > len(buf) {
				continue
			}
			secLen := int(buf[p+1]&0x0f)<<8 | int(buf[p+2])
			end := p + 3 + secLen - 4
			for q := p + 8; q+4 <= end && q+4 <= len(buf); q += 4 {
				if prog := int(buf[q])<<8 | int(buf[q+1]); prog != 0 {
					pmtPID = int(buf[q+2]&0x1f)<<8 | int(buf[q+3])
					break
				}
			}
		case pid == pmtPID && !sawPMT: // PMT
			if payloadStart {
				p += 1 + int(buf[p])
			}
			if p+12 > len(buf) {
				continue
			}
			sawPMT = true
			secLen := int(buf[p+1]&0x0f)<<8 | int(buf[p+2])
			infoLen := int(buf[p+10]&0x0f)<<8 | int(buf[p+11])
			end := p + 3 + secLen - 4
			for q := p + 12 + infoLen; q+5 <= end && q+5 <= len(buf); {
				streamType := buf[q]
				esPID := int(buf[q+1]&0x1f)<<8 | int(buf[q+2])
				esLen := int(buf[q+3]&0x0f)<<8 | int(buf[q+4])
				// 0x0f = AAC in ADTS, 0x11 = AAC in LATM. Anything else (0x06 private
				// data, which the Tapo cameras carry) is not an audio track hls.js waits
				// for, so it is ignored here.
				if streamType == 0x0f || streamType == 0x11 {
					audioPID = esPID
				}
				// 0x01/0x02 MPEG-1/2, 0x1b H.264, 0x24 HEVC.
				if streamType == 0x01 || streamType == 0x02 || streamType == 0x1b || streamType == 0x24 {
					videoPID = esPID
				}
				q += 5 + esLen
			}
		case audioPID >= 0 && pid == audioPID && res.audio != audioAlive:
			// Any ADTS sync word (0xFFFx with layer bits clear) means the track is alive.
			for k := p; k+1 < i+tsPacketLen && k+1 < len(buf); k++ {
				if buf[k] == 0xff && buf[k+1]&0xf6 == 0xf0 {
					res.audio = audioAlive
					break
				}
			}
		case videoPID >= 0 && pid == videoPID && payloadStart && !res.hasVideoPTS:
			// PES header: 00 00 01 <stream_id> <len16> <flags1> <flags2> <hdr_len>, with
			// a 5-byte PTS following when the high bit of PTS_DTS_flags is set.
			if p+14 <= len(buf) && buf[p] == 0 && buf[p+1] == 0 && buf[p+2] == 1 && buf[p+7]&0x80 != 0 {
				b := buf[p+9 : p+14]
				res.videoPTS90k = int64(b[0]&0x0e)<<29 | int64(b[1])<<22 |
					int64(b[2]&0xfe)<<14 | int64(b[3])<<7 | int64(b[4])>>1
				res.hasVideoPTS = true
			}
		}
	}

	switch {
	case res.audio == audioAlive:
		// already decided
	case !sawPMT:
		res.audio = audioUnknown
	case audioPID < 0:
		res.audio = audioNone
	default:
		res.audio = audioDeclaredEmpty
	}
	return res
}

// openSegmentReader returns a reader for a segment's bytes, following the same
// canonical-then-mirror-copy fallback that handleSegmentFile uses.
func (s *Server) openSegmentReader(seg *store.Segment) (io.ReadCloser, error) {
	if filepath.IsAbs(seg.Path) {
		if f, err := os.Open(seg.Path); err == nil {
			return f, nil
		}
	} else if s.registry != nil {
		if backend, err := s.registry.For(seg.LocationID); err == nil {
			if rc, _, err := backend.Open(seg.Path); err == nil {
				return rc, nil
			}
		}
	}
	for _, c := range s.segmentCopies(seg.ID) {
		if filepath.IsAbs(c.Path) {
			if f, err := os.Open(c.Path); err == nil {
				return f, nil
			}
			continue
		}
		if s.registry == nil {
			continue
		}
		backend, err := s.registry.For(c.LocationID)
		if err != nil {
			continue
		}
		if rc, _, err := backend.Open(c.Path); err == nil {
			return rc, nil
		}
	}
	return nil, os.ErrNotExist
}

// serveSegmentRepaired serves a segment with a silent audio track substituted for its dead
// one. It reports false when the segment needs no repair (or could not be read), so the
// caller falls through to its normal zero-copy handling.
//
// The decision is made per SEGMENT, never cached per camera: this archive has real audio
// before 2026-08-20 21:13, dead audio until 05:34 the next day, and real audio after, so a
// per-camera verdict would be wrong for most of it in one direction or the other.
func (s *Server) serveSegmentRepaired(w http.ResponseWriter, r *http.Request, seg *store.Segment) bool {
	rc, err := s.openSegmentReader(seg)
	if err != nil {
		return false
	}
	head, err := io.ReadAll(io.LimitReader(rc, segmentScanBytes))
	if err != nil || len(head) == 0 {
		rc.Close()
		return false
	}
	scan := scanTS(head)
	if scan.audio != audioDeclaredEmpty {
		rc.Close()
		return false // healthy, or nothing to repair — keep the fast path
	}

	rest, err := io.ReadAll(io.LimitReader(rc, maxRepairBytes-int64(len(head))))
	rc.Close()
	if err != nil {
		return false
	}
	src := append(head, rest...)
	// A segment at the cap is pathological; serve it untouched rather than silently
	// repairing a truncated prefix and presenting it as the whole thing.
	if int64(len(src)) >= maxRepairBytes {
		s.log.Warn("segment: too large to repair, serving original",
			"camera", seg.CameraID, "segment", seg.ID, "bytes", len(src))
		return false
	}

	select {
	case repairSem <- struct{}{}:
		defer func() { <-repairSem }()
	case <-r.Context().Done():
		return true // viewer went away while queued
	}

	ctx, cancel := context.WithTimeout(r.Context(), repairTimeout)
	defer cancel()

	// Video is stream-copied; only the dead track is replaced. -shortest stops at the end
	// of the video, since anullsrc is infinite. -copyts with muxdelay/muxpreload 0 keeps
	// the original timestamps so the repaired segment still lines up with the playlist's
	// EXTINF and EXT-X-PROGRAM-DATE-TIME timeline.
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-copyts", "-i", "pipe:0"}
	if scan.hasVideoPTS {
		// Start the silence where the video actually starts. Without this, ffmpeg
		// encodes silence from timestamp zero up to the video's PTS.
		args = append(args, "-itsoffset", strconv.FormatFloat(float64(scan.videoPTS90k)/90000, 'f', 6, 64))
	}
	args = append(args,
		"-f", "lavfi", "-i", "anullsrc=r=16000:cl=mono",
		"-map", "0:v", "-map", "1:a",
		"-c:v", "copy", "-c:a", "aac", "-b:a", "16k", "-shortest",
		"-muxdelay", "0", "-muxpreload", "0",
		"-f", "mpegts", "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdin = bytes.NewReader(src)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil || out.Len() == 0 {
		if r.Context().Err() != nil {
			return false // viewer went away; let the caller close out the response
		}
		// Fail safe: the original bytes beat a truncated or empty response.
		s.log.Warn("segment: audio repair failed, serving original", "camera", seg.CameraID,
			"segment", seg.ID, "err", err, "ffmpeg", errBuf.String())
		return false
	}

	http.ServeContent(w, r, seg.ID+".ts", time.UnixMilli(seg.StartTS), bytes.NewReader(out.Bytes()))
	return true
}
