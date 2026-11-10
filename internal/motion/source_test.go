package motion

import (
	"context"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

type fakeStreams struct{}

func (fakeStreams) RTSPURL(id string) string    { return "rtsp://go2rtc/cam_" + id }
func (fakeStreams) RTSPURLSub(id string) string { return "rtsp://go2rtc/cam_" + id + "_sub" }
func (fakeStreams) SnapshotCached(context.Context, string) ([]byte, error) {
	return nil, nil
}

// Motion only needs a 64x36 grid, so it must never decode a 4K main stream when the
// camera has a sub stream: that cost a core and half a gigabyte per camera.
func TestMotionSourcePrefersSubStream(t *testing.T) {
	withSub := store.Camera{ID: "a", RTSPMainURL: "rtsp://cam/main", RTSPSubURL: "rtsp://cam/sub"}
	if got := motionSource(fakeStreams{}, withSub); got != "rtsp://go2rtc/cam_a_sub" {
		t.Fatalf("camera with a sub stream: got %q, want the sub restream", got)
	}
	mainOnly := store.Camera{ID: "b", RTSPMainURL: "rtsp://cam/main"}
	if got := motionSource(fakeStreams{}, mainOnly); got != "rtsp://go2rtc/cam_b" {
		t.Fatalf("camera without a sub stream: got %q, want the main restream", got)
	}
}
