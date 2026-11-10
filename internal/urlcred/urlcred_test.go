package urlcred

import (
	"errors"
	"strings"
	"testing"
)

const (
	reolink = "rtsp://admin:pw@10.0.0.5:554/Preview_01_main"
	// go2rtc link options must survive byte-for-byte: net/url would escape the
	// second "#" and go2rtc would no longer see audio=copy.
	withOpts = "rtsp://aperture:s3cret@10.0.0.7:554/h265Preview_01_main#video=copy#audio=copy"
	// Cameras accept passwords pasted raw; net/url (and so go2rtc) splits the login
	// at the LAST "@" of the authority.
	rawAt = "rtsp://admin:p@ss:w0rd@10.0.0.8/live0"
)

func TestRedact(t *testing.T) {
	cases := []struct{ in, want string }{
		{reolink, "rtsp://•••••:•••••@10.0.0.5:554/Preview_01_main"},
		{withOpts, "rtsp://•••••:•••••@10.0.0.7:554/h265Preview_01_main#video=copy#audio=copy"},
		{rawAt, "rtsp://•••••:•••••@10.0.0.8/live0"},
		{"rtsp://admin@10.0.0.5/x", "rtsp://•••••@10.0.0.5/x"},
		{"rtsp://10.0.0.5/x", "rtsp://10.0.0.5/x"},
		{"rtsp://10.0.0.5/a@b", "rtsp://10.0.0.5/a@b"}, // "@" in the path is not a login
		{"rtsp://[fe80::1]:554/x", "rtsp://[fe80::1]:554/x"},
		{"rtsp://u:p@[fe80::1]:554/x", "rtsp://•••••:•••••@[fe80::1]:554/x"},
		{"", ""},
		{"not a link", "not a link"},
	}
	for _, c := range cases {
		if got := Redact(c.in); got != c.want {
			t.Errorf("Redact(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// The placeholder must not reveal how long the real values are.
	if Redact("rtsp://u:x@h/p") != Redact("rtsp://averylonguser:averyveryverylongpassword@h/p") {
		t.Error("the redacted form depends on the credential lengths")
	}
}

func TestMergeUntouchedRoundTrips(t *testing.T) {
	for _, s := range []string{reolink, withOpts, rawAt, "rtsp://admin@10.0.0.5/x", "rtsp://u:p@[fe80::1]:554/x"} {
		got, err := Merge(Redact(s), s)
		if err != nil || got != s {
			t.Errorf("Merge(Redact(%q)) = %q, %v; want the stored link back", s, got, err)
		}
	}
}

func TestMerge(t *testing.T) {
	cases := []struct {
		name      string
		submitted string
		stored    []string
		want      string
		err       error
	}{
		{"path change keeps the login", "rtsp://•••••:•••••@10.0.0.5:554/Preview_01_sub", []string{reolink}, "rtsp://admin:pw@10.0.0.5:554/Preview_01_sub", nil},
		{"port change on the same host keeps it", "rtsp://•••••:•••••@10.0.0.5:8554/x", []string{reolink}, "rtsp://admin:pw@10.0.0.5:8554/x", nil},
		{"host names compare case-insensitively", "rtsp://•••••:•••••@CAM.local/x", []string{"rtsp://a:b@cam.local/x"}, "rtsp://a:b@CAM.local/x", nil},
		{"new password only", "rtsp://•••••:newpw@10.0.0.5/x", []string{reolink}, "rtsp://admin:newpw@10.0.0.5/x", nil},
		{"new username only", "rtsp://bob:•••••@10.0.0.5/x", []string{reolink}, "rtsp://bob:pw@10.0.0.5/x", nil},
		{"password removed", "rtsp://•••••@10.0.0.5/x", []string{reolink}, "rtsp://admin@10.0.0.5/x", nil},
		{"typed link taken as is", "rtsp://new:login@10.9.9.9/x", []string{reolink}, "rtsp://new:login@10.9.9.9/x", nil},
		{"login deleted", "rtsp://10.0.0.5/x", []string{reolink}, "rtsp://10.0.0.5/x", nil},
		{"raw @ in a new password", "rtsp://•••••:n@w@10.0.0.5/x", []string{reolink}, "rtsp://admin:n@w@10.0.0.5/x", nil},

		{"new address refuses the saved login", "rtsp://•••••:•••••@10.6.6.6/x", []string{reolink}, "", ErrNewAddress},
		{"half-kept login on a new address too", "rtsp://•••••:newpw@10.6.6.6/x", []string{reolink}, "", ErrNewAddress},
		{"partly overtyped dots", "rtsp://••x••:•••••@10.0.0.5/x", []string{reolink}, "", ErrEditedMask},
		{"dots outside the login", "rtsp://10.0.0.5/live/•••••", []string{reolink}, "", ErrMaskOutsideLogin},
		// A path that is only the placeholder stands in for a stream key (TestStreamKey).
		{"dots as the whole path, no key saved", "rtsp://10.0.0.5/•••••", []string{reolink}, "", ErrNoKeyStored},
		{"nothing saved", "rtsp://•••••:•••••@10.0.0.5/x", []string{"rtsp://10.0.0.5/x"}, "", ErrNothingStored},
		{"nothing saved at all", "rtsp://•••••:•••••@10.0.0.5/x", []string{""}, "", ErrNothingStored},
		{"no saved password", "rtsp://•••••:•••••@10.0.0.5/x", []string{"rtsp://user@10.0.0.5/x"}, "", ErrNothingStored},

		{"sub pasted from main borrows its login", "rtsp://•••••:•••••@10.0.0.5:554/Preview_01_sub", []string{"", reolink}, "rtsp://admin:pw@10.0.0.5:554/Preview_01_sub", nil},
		{"sibling on another host lends nothing", "rtsp://•••••:•••••@10.0.0.9/sub", []string{"", reolink}, "", ErrNewAddress},
		{"login saved for this host wins", "rtsp://•••••:•••••@10.0.0.5/x", []string{"rtsp://old:one@10.0.0.4/x", reolink}, "rtsp://admin:pw@10.0.0.5/x", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Merge(c.submitted, c.stored...)
			if !errors.Is(err, c.err) || got != c.want {
				t.Fatalf("Merge(%q) = %q, %v; want %q, %v", c.submitted, got, err, c.want, c.err)
			}
		})
	}
}

func TestFollow(t *testing.T) {
	const (
		oldMain = "rtsp://admin:old@10.0.0.5/Preview_01_main"
		newMain = "rtsp://admin:new@10.0.0.5/Preview_01_main"
	)
	cases := []struct{ name, sub, oldMain, newMain, want string }{
		{"shared login follows", "rtsp://admin:old@10.0.0.5/Preview_01_sub#video=copy", oldMain, newMain, "rtsp://admin:new@10.0.0.5/Preview_01_sub#video=copy"},
		{"own login stays", "rtsp://viewer:other@10.0.0.5/sub", oldMain, newMain, "rtsp://viewer:other@10.0.0.5/sub"},
		{"unchanged main changes nothing", "rtsp://admin:old@10.0.0.5/sub", oldMain, oldMain, "rtsp://admin:old@10.0.0.5/sub"},
		{"never carried to another host", "rtsp://admin:old@10.0.0.5/sub", oldMain, "rtsp://admin:new@10.0.0.6/main", "rtsp://admin:old@10.0.0.5/sub"},
		{"login added to both", "rtsp://10.0.0.5/sub", "rtsp://10.0.0.5/main", newMain, "rtsp://admin:new@10.0.0.5/sub"},
		{"no sub", "", oldMain, newMain, ""},
	}
	for _, c := range cases {
		if got := Follow(c.sub, c.oldMain, c.newMain); got != c.want {
			t.Errorf("%s: Follow = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestScrub(t *testing.T) {
	cases := []struct{ in, want string }{
		{`error=EOF url=rtsp://admin:pw@192.168.10.38/Preview_01_main`, `error=EOF url=rtsp://•••••:•••••@192.168.10.38/Preview_01_main`},
		{`dial rtsp://a:b@h1/x and rtsp://c@h2`, `dial rtsp://•••••:•••••@h1/x and rtsp://•••••@h2`},
		{`"rtsp://admin:p@ss@10.0.0.8/live0"`, `"rtsp://•••••:•••••@10.0.0.8/live0"`},
		{`no links, mail me at a@b.com`, `no links, mail me at a@b.com`},
		{`rtsp://10.0.0.5/a@b`, `rtsp://10.0.0.5/a@b`},
	}
	for _, c := range cases {
		if got := Scrub(c.in); got != c.want {
			t.Errorf("Scrub(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if strings.Contains(Scrub(rawAt), "ss:w0rd") {
		t.Error("the part of a password after a raw @ leaked")
	}
}

// UniFi Protect links have no login: the path is the stream key, and anyone who
// has it can watch the camera. It gets the same treatment as a password.
func TestStreamKey(t *testing.T) {
	const key = "AbCdEfGhIjKlMnOp"
	main := "rtspx://192.168.10.1:7441/" + key
	sub := "rtspx://192.168.10.1:7441/QrStUvWxYz012345"

	if got, want := Redact(main), "rtspx://192.168.10.1:7441/"+Mask; got != want {
		t.Errorf("Redact = %q, want %q", got, want)
	}
	if got, want := Redact(main+"?enableSrtp"), "rtspx://192.168.10.1:7441/"+Mask+"?enableSrtp"; got != want {
		t.Errorf("Redact with a query = %q, want %q", got, want)
	}
	for _, plain := range []string{
		"rtsp://u:p@10.0.0.5:554/Preview_01_main",
		"rtsp://10.0.0.5:554/stream1",
		"rtsp://10.0.0.5:554/Streaming/Channels/101",
		"rtsp://10.0.0.5/cam/realmonitor?channel=1&subtype=0",
	} {
		if got := Redact(plain); HasMask(got) && !strings.Contains(plain, "@") {
			t.Errorf("Redact(%q) masked a path that isn't a key: %q", plain, got)
		}
	}

	for _, c := range []struct {
		name, submitted string
		stored          []string
		want            string
		err             error
	}{
		{"keeps the field's own key", Redact(main), []string{main, sub}, main, nil},
		{"sub keeps its own key", Redact(sub), []string{sub, main}, sub, nil},
		{"a new key is taken as typed", "rtspx://192.168.10.1:7441/ZZZZZZZZZZZZZZZZ", []string{main}, "rtspx://192.168.10.1:7441/ZZZZZZZZZZZZZZZZ", nil},
		{"never to another host", "rtspx://192.168.66.6:7441/" + Mask, []string{main}, "", ErrKeyNewAddress},
		{"nothing saved", "rtspx://192.168.10.1:7441/" + Mask, []string{"rtsp://u:p@10.0.0.5/stream1"}, "", ErrNoKeyStored},
		{"half overtyped", "rtspx://192.168.10.1:7441/••x••", []string{main}, "", ErrEditedMask},
		{"dots in another segment", "rtspx://192.168.10.1:7441/a/" + Mask, []string{main}, "", ErrMaskOutsideLogin},
		{"dots in the host", "rtspx://192.168.10.•:7441/" + Mask, []string{main}, "", ErrMaskOutsideLogin},
	} {
		got, err := Merge(c.submitted, c.stored...)
		if !errors.Is(err, c.err) || got != c.want {
			t.Errorf("%s: Merge = %q, %v; want %q, %v", c.name, got, err, c.want, c.err)
		}
	}

	// A login and a key on one link are both kept.
	both := "rtsps://admin:pw@10.0.0.9:7441/" + key
	if got, err := Merge(Redact(both), both); err != nil || got != both {
		t.Errorf("login and key: Merge = %q, %v", got, err)
	}

	msg := `streams: dial "rtspx://192.168.10.1:7441/` + key + `": i/o timeout`
	if got := Scrub(msg); strings.Contains(got, key) {
		t.Errorf("Scrub left the stream key in %q", got)
	}
	if got := Scrub("rtspx://192.168.10.1:7441/" + key); strings.Contains(got, key) {
		t.Errorf("Scrub left a key at the end of the text: %q", got)
	}
}
