package api

import (
	"os"
	"path/filepath"
	"testing"
)

// buildTS assembles a minimal but structurally valid MPEG-TS prefix: a PAT pointing at a
// PMT, a PMT declaring the given elementary streams, and one payload packet per PID.
func buildTS(streams []struct {
	typ byte
	pid int
}, audioPayload []byte) []byte {
	pkt := func(pid int, payloadStart bool, body []byte) []byte {
		p := make([]byte, tsPacketLen)
		for i := range p {
			p[i] = 0xff
		}
		p[0] = 0x47
		p[1] = byte(pid >> 8)
		if payloadStart {
			p[1] |= 0x40
		}
		p[2] = byte(pid)
		p[3] = 0x10 // payload only
		copy(p[4:], body)
		return p
	}

	// PAT: one program, PMT on PID 4096.
	pat := []byte{0x00, 0x00, 0xb0, 0x0d, 0x00, 0x01, 0xc1, 0x00, 0x00, 0x00, 0x01, 0xf0, 0x00, 0, 0, 0, 0}
	pat[0] = 0x00 // pointer_field

	// PMT body after the pointer field.
	var es []byte
	for _, s := range streams {
		es = append(es, s.typ, byte(0xe0|s.pid>>8), byte(s.pid), 0xf0, 0x00)
	}
	secLen := 9 + len(es) + 4
	pmt := []byte{0x00, 0x02, byte(0xb0 | secLen>>8), byte(secLen), 0x00, 0x01, 0xc1, 0x00, 0x00, 0xe1, 0x00, 0xf0, 0x00}
	pmt = append(pmt, es...)
	pmt = append(pmt, 0, 0, 0, 0) // CRC

	out := pkt(0, true, pat)
	out = append(out, pkt(4096, true, pmt)...)
	for _, s := range streams {
		body := []byte{0x00, 0x00, 0x01, 0xc0, 0x00, 0x00, 0x80, 0x00, 0x00}
		if s.typ == 0x0f || s.typ == 0x11 {
			body = append(body, audioPayload...)
		}
		out = append(out, pkt(s.pid, true, body)...)
	}
	return out
}

type esDecl = struct {
	typ byte
	pid int
}

func TestScanTSAudio(t *testing.T) {
	adts := []byte{0xff, 0xf1, 0x60, 0x40, 0x20, 0x7f, 0xfc} // real header from a working segment
	silence := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	cases := []struct {
		name    string
		streams []esDecl
		payload []byte
		want    tsAudioState
		why     string
	}{
		{
			name:    "declared AAC carrying ADTS is alive",
			streams: []esDecl{{0x24, 256}, {0x0f, 257}},
			payload: adts,
			want:    audioAlive,
			why:     "pre-2026-08-20 21:13 footage and everything after the recorder fix",
		},
		{
			name:    "declared AAC with no ADTS is the dead-track case",
			streams: []esDecl{{0x24, 256}, {0x0f, 257}},
			payload: silence,
			want:    audioDeclaredEmpty,
			why:     "what ffmpeg 7.1.5 -c copy produced; this is what wedges hls.js",
		},
		{
			name:    "no audio stream declared needs no repair",
			streams: []esDecl{{0x1b, 256}},
			payload: nil,
			want:    audioNone,
			why:     "a camera with video only",
		},
		{
			name:    "private data is not an audio track",
			streams: []esDecl{{0x1b, 256}, {0x06, 257}},
			payload: nil,
			want:    audioNone,
			why:     "the Tapo cameras carry 0x06 metadata; hls.js ignores it, so must we",
		},
		{
			name:    "LATM audio is recognised as audio",
			streams: []esDecl{{0x1b, 256}, {0x11, 257}},
			payload: silence,
			want:    audioDeclaredEmpty,
			why:     "0x11 is still a track hls.js will wait for",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scanTS(buildTS(tc.streams, tc.payload)).audio; got != tc.want {
				t.Errorf("scanTS().audio = %v, want %v (%s)", got, tc.want, tc.why)
			}
		})
	}
}

func TestScanTSAudioRejectsGarbage(t *testing.T) {
	// No PMT must mean "leave it alone", never "repair it" — an unparseable prefix is not
	// evidence of a dead track, and repairing on a guess would rewrite healthy footage.
	for _, tc := range []struct {
		name string
		in   []byte
	}{
		{"empty", nil},
		{"short", []byte{0x47, 0x00}},
		{"no sync byte", make([]byte, 4*tsPacketLen)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := scanTS(tc.in).audio; got != audioUnknown {
				t.Errorf("scanTS().audio = %v, want audioUnknown", got)
			}
		})
	}
}

// TestScanTSAudioAgainstRealSegments runs the scanner over actual recorded segments when
// this test is run on the NVR host. It is skipped elsewhere.
func TestScanTSAudioAgainstRealSegments(t *testing.T) {
	const recDir = "/var/lib/aperture/recordings"
	if _, err := os.Stat(recDir); err != nil {
		t.Skip("not running on the NVR host")
	}
	matches, _ := filepath.Glob(filepath.Join(recDir, "*", "*.ts"))
	if len(matches) == 0 {
		t.Skip("no segments on disk")
	}
	checked := 0
	for _, p := range matches {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		buf := make([]byte, segmentScanBytes)
		n, _ := f.Read(buf)
		f.Close()
		if n == 0 {
			continue
		}
		// Whatever the verdict, it must be a decided one — a real segment should always
		// parse far enough to find its PMT.
		if got := scanTS(buf[:n]).audio; got == audioUnknown {
			t.Errorf("%s: scanner could not find a PMT in %d bytes", filepath.Base(p), n)
		}
		if checked++; checked >= 25 {
			break
		}
	}
	t.Logf("scanned %d real segments", checked)
}
