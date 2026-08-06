package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestHLSProgramDateTime covers T2.2: the VOD playlist carries an
// EXT-X-PROGRAM-DATE-TIME anchored to the exact segment start_ts before
// segment 0 and re-emitted after each discontinuity, and nowhere else.
func TestHLSProgramDateTime(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	const T = int64(1_700_000_000_000)
	// Three contiguous, then a >1s gap (discontinuity), then two contiguous.
	segs := []struct{ start, end int64 }{
		{T, T + 4_000},
		{T + 4_000, T + 8_000},
		{T + 8_000, T + 12_000},
		{T + 60_000, T + 64_000}, // gap -> discontinuity + new PDT anchor
		{T + 64_000, T + 68_000},
	}
	for _, sg := range segs {
		mustDo(t, s.st.InsertSegment(&store.Segment{CameraID: "cam1", Path: "x", StartTS: sg.start, EndTS: sg.end, DurationMS: sg.end - sg.start}))
	}

	rec := s.reqAs("GET", fmt.Sprintf("/api/v1/cameras/cam1/hls?from=%d&to=%d", T-1, T+100_000), adminC)
	if rec.Code != http.StatusOK {
		t.Fatalf("HLS: got %d, want 200", rec.Code)
	}
	pl := rec.Body.String()

	pdt := func(ts int64) string {
		return "#EXT-X-PROGRAM-DATE-TIME:" + time.UnixMilli(ts).UTC().Format("2006-01-02T15:04:05.000Z07:00")
	}
	// Exactly two anchors: segment 0 and the post-discontinuity segment.
	if n := strings.Count(pl, "#EXT-X-PROGRAM-DATE-TIME:"); n != 2 {
		t.Fatalf("PDT tag count = %d, want 2 (seg0 + after discontinuity)\n%s", n, pl)
	}
	if !strings.Contains(pl, pdt(T)) {
		t.Errorf("missing PDT anchor at segment 0 (%s)", pdt(T))
	}
	if !strings.Contains(pl, pdt(T+60_000)) {
		t.Errorf("missing PDT anchor after discontinuity (%s)", pdt(T+60_000))
	}
	// The anchor must be at exactly one discontinuity, and the PDT line must be
	// the line immediately following the DISCONTINUITY marker.
	lines := strings.Split(pl, "\n")
	discCount := 0
	for i, ln := range lines {
		if ln != "#EXT-X-DISCONTINUITY" {
			continue
		}
		discCount++
		if i+1 >= len(lines) || lines[i+1] != pdt(T+60_000) {
			t.Errorf("line after discontinuity = %q, want the PDT anchor %q", safeIdx(lines, i+1), pdt(T+60_000))
		}
	}
	if discCount != 1 {
		t.Errorf("discontinuity count = %d, want 1", discCount)
	}
}

func safeIdx(s []string, i int) string {
	if i < 0 || i >= len(s) {
		return "<out of range>"
	}
	return s[i]
}

// TestPlaybackSwitch covers the runtime seek switch: default off in
// capabilities, flippable via PUT /system/playback with no redeploy.
func TestPlaybackSwitch(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	feature := func() bool {
		t.Helper()
		rec := s.reqAs("GET", "/api/v1/capabilities", adminC)
		if rec.Code != http.StatusOK {
			t.Fatalf("capabilities: got %d, want 200", rec.Code)
		}
		var body struct {
			Features map[string]bool `json:"features"`
		}
		mustDo(t, json.Unmarshal(rec.Body.Bytes(), &body))
		return body.Features["pdtSeek"]
	}

	if feature() {
		t.Fatal("pdtSeek should default to false")
	}
	// The 2.A capabilities that are live should read true.
	rec := s.reqAs("GET", "/api/v1/capabilities", adminC)
	var body struct {
		Features map[string]bool `json:"features"`
	}
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &body))
	if !body.Features["coverageEndpoint"] || !body.Features["cursorPagination"] {
		t.Errorf("coverageEndpoint/cursorPagination should be true after 2.A: %+v", body.Features)
	}

	// Flip on via the runtime switch.
	put := s.reqBody("PUT", "/api/v1/system/playback", adminC, strings.NewReader(`{"pdtSeek":true}`))
	if put.Code != http.StatusOK {
		t.Fatalf("PUT playback: got %d, want 200", put.Code)
	}
	if !feature() {
		t.Fatal("pdtSeek should be true after flipping on")
	}
	// And back off.
	s.reqBody("PUT", "/api/v1/system/playback", adminC, strings.NewReader(`{"pdtSeek":false}`))
	if feature() {
		t.Fatal("pdtSeek should be false after flipping off")
	}
}
