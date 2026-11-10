package stream

import "testing"

func TestProtectLinks(t *testing.T) {
	const key = "AbCdEfGhIjKlMnOp"
	for _, c := range []struct{ in, want string }{
		{"rtsps://192.168.10.1:7441/" + key + "?enableSrtp", "rtspx://192.168.10.1:7441/" + key},
		{"  rtsps://192.168.10.1:7441/" + key + "?enableSrtp  ", "rtspx://192.168.10.1:7441/" + key},
		{"rtsps://192.168.10.1:7441/" + key, "rtspx://192.168.10.1:7441/" + key},
		{"rtsps://192.168.10.1:7441/" + key + "?enableSrtp&x=1", "rtspx://192.168.10.1:7441/" + key + "?x=1"},
		{"rtsps://192.168.10.1:7441/" + key + "?x=1&enableSrtp", "rtspx://192.168.10.1:7441/" + key + "?x=1"},
		{"rtspx://192.168.10.1:7441/" + key, "rtspx://192.168.10.1:7441/" + key},
		{"rtsp://192.168.10.1:7447/" + key, "rtsp://192.168.10.1:7447/" + key},
		// Not Protect: left exactly as typed.
		{"rtsps://cam.example:322/live/ch0", "rtsps://cam.example:322/live/ch0"},
		{"rtsp://u:p@10.0.0.5:554/Preview_01_main", "rtsp://u:p@10.0.0.5:554/Preview_01_main"},
	} {
		if got := NormalizeSource(c.in); got != c.want {
			t.Errorf("NormalizeSource(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if !IsProtectSource("rtspx://192.168.10.1:7441/"+key) || IsProtectSource("rtsp://10.0.0.5:554/stream1") {
		t.Error("IsProtectSource misjudged a link")
	}
	for link, want := range map[string]bool{
		"rtsp://a/b": true, "rtsps://a/b": true, "rtspx://a/b": true, "RTSP://a/b": true,
		"http://a/b": false, "exec:ffmpeg -i x": false, "": false,
	} {
		if playable(link) != want {
			t.Errorf("playable(%q) = %v", link, !want)
		}
	}
}
