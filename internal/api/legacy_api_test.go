package api

import "testing"

// TestNormalizeAPIPath covers the id-collapsing so per-resource alias hits group
// by route shape.
func TestNormalizeAPIPath(t *testing.T) {
	cases := map[string]string{
		"/api/cameras/4dc6bf3f3699967fcfd2abc16cfee84c/hls":     "/api/cameras/:id/hls",
		"/api/events/49944e3b164bad75f0719d02f4098de0/thumb.jpg": "/api/events/:id/thumb.jpg",
		"/api/cameras":         "/api/cameras",
		"/api/system/health":   "/api/system/health",
		"/api/users/42":        "/api/users/:id",
		"/api/system/settings": "/api/system/settings",
	}
	for in, want := range cases {
		if got := normalizeAPIPath(in); got != want {
			t.Errorf("normalizeAPIPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLegacyAPICounterViaMiddleware covers Item 1: hits on the deprecated /api
// alias are counted by path; /api/v1 hits are not.
func TestLegacyAPICounterViaMiddleware(t *testing.T) {
	s := newTestServer(t)
	anon := cred{"anon", nil}
	// Two hits on the deprecated alias (public route → 200 through the middleware).
	s.reqAs("GET", "/api/system/health", anon)
	s.reqAs("GET", "/api/system/health", anon)
	// A hit on the v1 mount must NOT be counted.
	s.reqAs("GET", "/api/v1/system/health", anon)

	byPath, total := s.legacyAPI.snapshot()
	if total != 2 {
		t.Fatalf("legacy alias total = %d, want 2 (v1 excluded)", total)
	}
	if byPath["/api/system/health"] != 2 {
		t.Errorf("byPath = %v, want /api/system/health -> 2", byPath)
	}
}

// TestLegacyAPICounterCap covers the distinct-key cap so a sprayed path set
// can't grow the map unbounded.
func TestLegacyAPICounterCap(t *testing.T) {
	c := newLegacyAPICounter()
	for i := 0; i < 500; i++ {
		// each a distinct non-id path so normalization can't collapse them
		c.count("/api/x" + string(rune('a'+i%26)) + "/y" + itoa(i))
	}
	byPath, total := c.snapshot()
	if total != 500 {
		t.Errorf("total = %d, want 500", total)
	}
	if len(byPath) > 201 { // 200 distinct + "(other)"
		t.Errorf("distinct keys = %d, want <= 201 (capped)", len(byPath))
	}
	if byPath["(other)"] == 0 {
		t.Errorf("overflow should land in (other)")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
