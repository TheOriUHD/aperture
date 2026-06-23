package stream

import (
	"encoding/json"
	"testing"
)

// TestSameSources guards the anti-blip optimization: an edit that doesn't change
// a camera's RTSP sources must be detected as unchanged so we never re-register
// the go2rtc stream (which would drop active live viewers).
func TestSameSources(t *testing.T) {
	two := json.RawMessage(`{"producers":[{"url":"rtsp://a/1"},{"url":"rtsp://a/2"}]}`)
	cases := []struct {
		name string
		raw  json.RawMessage
		srcs []string
		want bool
	}{
		{"same order", two, []string{"rtsp://a/1", "rtsp://a/2"}, true},
		{"different order", two, []string{"rtsp://a/2", "rtsp://a/1"}, true},
		{"fewer sources", two, []string{"rtsp://a/1"}, false},
		{"changed url", two, []string{"rtsp://a/1", "rtsp://a/3"}, false},
		{"single match", json.RawMessage(`{"producers":[{"url":"rtsp://x/main"}]}`), []string{"rtsp://x/main"}, true},
		{"bad json", json.RawMessage(`nope`), []string{"x"}, false},
	}
	for _, tc := range cases {
		if got := sameSources(tc.raw, tc.srcs); got != tc.want {
			t.Errorf("%s: sameSources = %v, want %v", tc.name, got, tc.want)
		}
	}
}
