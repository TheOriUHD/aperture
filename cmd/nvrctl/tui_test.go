package main

import (
	"testing"
	"unicode/utf8"
)

// TestTruncCountsCharacters pins that the camera list keeps a masked link's
// address visible: "•" is 3 bytes, so a byte count would cut right after the
// login and hide the host.
func TestTruncCountsCharacters(t *testing.T) {
	link := "rtsp://•••••:•••••@192.168.10.38/Preview_01_main"
	if got := trunc(link, 40); got != "rtsp://•••••:•••••@192.168.10.38/Previe…" {
		t.Errorf("trunc(link, 40) = %q", got)
	}
	for _, s := range []string{link, "Küche Küche Küche"} {
		for n := 0; n < 45; n++ {
			if got := trunc(s, n); !utf8.ValidString(got) {
				t.Fatalf("trunc(%q, %d) cut a character in half: %q", s, n, got)
			}
		}
	}
	if trunc("short", 40) != "short" {
		t.Error("a short string must be returned unchanged")
	}
}
