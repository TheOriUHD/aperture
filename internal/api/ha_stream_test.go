package api

import (
	"net/http/httptest"
	"testing"
)

// TestAdvertisedRTSPHost pins the reverse-proxy fix: the RTSP host handed to external
// consumers comes from the configured StreamHost when set (the address go2rtc is
// actually reachable at), and only falls back to the request host when it is not — so
// a proxy domain never produces an unreachable rtsp://<domain>:8554 URL.
func TestAdvertisedRTSPHost(t *testing.T) {
	s := newTestServer(t)

	// Reverse-proxy: the browser reached Aperture at a domain, but StreamHost names the
	// LAN IP where go2rtc's RTSP port is reachable.
	s.cfg.StreamHost = "192.168.10.147"
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "aperture.example.com"
	if got := s.advertisedRTSPHost(req); got != "192.168.10.147:8554" {
		t.Fatalf("StreamHost set: got %q, want 192.168.10.147:8554", got)
	}

	// A StreamHost that already carries a port is used verbatim.
	s.cfg.StreamHost = "cam.lan:1234"
	if got := s.advertisedRTSPHost(req); got != "cam.lan:1234" {
		t.Fatalf("host:port passthrough: got %q", got)
	}

	// Unset → fall back to the host the client used (correct for direct LAN-IP access)
	// with go2rtc's RTSP port appended.
	s.cfg.StreamHost = ""
	req.Host = "192.168.10.147:8971"
	if got := s.advertisedRTSPHost(req); got != "192.168.10.147:8554" {
		t.Fatalf("fallback: got %q, want 192.168.10.147:8554", got)
	}
}
