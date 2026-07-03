package onvif

import (
	"strings"

	"github.com/TheOriUHD/aperture/internal/store"
)

// camView is the subset of camera data the ONVIF payload builders need, with
// resolution/framerate defaults filled in. Aperture's store.Camera does not
// persist the negotiated stream resolution (go2rtc handles that), so we advertise
// a conventional 1080p/relative default; ONVIF clients treat these as advisory
// and pull true parameters from the RTSP SDP on connect.
type camView struct {
	ID     string
	Name   string
	Width  int
	Height int
	FPS    int
}

// view projects a store.Camera into a camView with defaults applied.
func view(c *store.Camera) camView {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = "Camera " + c.ID
	}
	fps := c.FPS
	if fps <= 0 {
		fps = 15
	}
	return camView{
		ID:     c.ID,
		Name:   name,
		Width:  1920,
		Height: 1080,
		FPS:    fps,
	}
}

// xmlEscape escapes text for inclusion in element content or double-quoted
// attribute values. We hand-roll it (rather than xml.EscapeText) so the builders
// can stay string-based without a bytes.Buffer per field.
func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}
