package ext

import "testing"

const playerSlotBase = `"manifest":1,"id":"a.b","name":"N","version":"1.0.0"`

func parseValidate(t *testing.T, js string) error {
	t.Helper()
	m, err := Parse([]byte(js))
	if err != nil {
		return err
	}
	_, err = m.Validate()
	return err
}

// The player slots put extension UI ON the video: an icon in the control cluster
// and a widget floating over the picture. Both are camera-context, so both must be
// accepted by the manifest and both must be gated by camera grant on the host side
// (see web/src/ext/panels.test.ts for that half).
func TestPlayerSlotsAccepted(t *testing.T) {
	for _, slot := range []string{"player-toolbar", "player-osd"} {
		t.Run(slot, func(t *testing.T) {
			js := `{` + playerSlotBase + `,"permissions":{"cameras":"all","data":{"cameras":"read"}},
				"ui":{"panels":[{"slot":"` + slot + `","entry":"ui/p.html","title":"T"}]}}`
			if err := parseValidate(t, js); err != nil {
				t.Fatalf("slot %q should validate, got %v", slot, err)
			}
		})
	}
}

func TestPlayerOSDAnchor(t *testing.T) {
	cases := []struct {
		name    string
		slot    string
		anchor  string
		wantErr bool
		why     string
	}{
		{"valid corner", "player-osd", "bottom-right", false, "the four corners are the whole vocabulary"},
		{"top-left", "player-osd", "top-left", false, ""},
		{"absent anchor defaults", "player-osd", "", false, "empty means bottom-left, decided by the host"},
		{"unknown corner", "player-osd", "middle", true, "a typo must fail at install, not place the widget somewhere arbitrary"},
		{"coordinates rejected", "player-osd", "12,40", true, "extensions declare a corner, never coordinates"},
		{
			"anchor on a non-OSD slot", "player-toolbar", "top-left", true,
			"a manifest must not claim a placement the host will silently ignore",
		},
		{"anchor on a card slot", "dashboard-card", "top-left", true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			anchor := ""
			if tc.anchor != "" {
				anchor = `,"anchor":"` + tc.anchor + `"`
			}
			js := `{` + playerSlotBase + `,"permissions":{"cameras":"all","data":{"cameras":"read"}},
				"ui":{"panels":[{"slot":"` + tc.slot + `","entry":"ui/p.html"` + anchor + `}]}}`
			err := parseValidate(t, js)
			if tc.wantErr && err == nil {
				t.Fatalf("expected an error (%s)", tc.why)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected success (%s), got %v", tc.why, err)
			}
		})
	}
}

// A player slot is camera-context, so an extension using one has to have asked for
// cameras — the same coherence rule that already covers camera-detail. Without it an
// extension could paint controls onto a camera it was never granted.
func TestPlayerSlotWithoutCameraRequest(t *testing.T) {
	js := `{` + playerSlotBase + `,"permissions":{"data":{"cameras":"read"}},
		"ui":{"panels":[{"slot":"player-osd","entry":"ui/p.html"}]}}`
	m, err := Parse([]byte(js))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d, err := m.Validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !CameraDataWithoutRequest(d) {
		t.Fatal("camera data without a camera request must be caught at install")
	}
}
