package onvifclient

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DeviceInfo is the camera's self-description.
type DeviceInfo struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Firmware     string `json:"firmware"`
	Serial       string `json:"serial"`
}

// Range is an adjustable setting's bounds as the camera reports them. Vendors
// use different scales (0–255 here, 0–100 there), so the UI must render what the
// camera says rather than assume.
type Range struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// Imaging is the current picture settings plus what may be changed. A nil
// pointer means "this camera does not expose that control" — the UI hides it
// rather than showing a dead slider.
type Imaging struct {
	Brightness  *float64 `json:"brightness,omitempty"`
	Contrast    *float64 `json:"contrast,omitempty"`
	Saturation  *float64 `json:"saturation,omitempty"`
	Sharpness   *float64 `json:"sharpness,omitempty"`
	IrCutFilter string   `json:"irCutFilter,omitempty"` // ON | OFF | AUTO

	Ranges       map[string]Range `json:"ranges"`
	IrCutOptions []string         `json:"irCutOptions,omitempty"`
}

// Probe finds a camera's ONVIF service and reports what it offers. It walks the
// common ports (vendors disagree) and stops at the first that answers.
type ProbeResult struct {
	OK          bool       `json:"ok"`
	Port        int        `json:"port"`
	Device      DeviceInfo `json:"device"`
	HasImaging  bool       `json:"hasImaging"`
	HasPTZ      bool       `json:"hasPtz"`
	VideoSource string     `json:"videoSource"`
	Error       string     `json:"error,omitempty"`
}

// Probe tries each candidate port until one answers ONVIF. Ports that aren't
// even open are skipped with a fast TCP check, so a full probe stays quick.
func Probe(ctx context.Context, host, user, pass string, ports []int, timeout time.Duration) ProbeResult {
	if len(ports) == 0 {
		ports = DefaultPorts
	}
	var lastErr string
	for _, p := range ports {
		if !reachable(ctx, host, p, 900*time.Millisecond) {
			continue
		}
		c := New(fmt.Sprintf("%s:%d", host, p), user, pass, timeout)
		caps, err := c.call(ctx, "device_service",
			`<tds:GetCapabilities><tds:Category>All</tds:Category></tds:GetCapabilities>`)
		if err != nil && caps == "" {
			lastErr = err.Error()
			continue
		}
		res := ProbeResult{
			OK:         true,
			Port:       p,
			HasImaging: strings.Contains(caps, "imaging_service") || strings.Contains(caps, "Imaging"),
			HasPTZ:     strings.Contains(caps, "ptz_service") || strings.Contains(caps, "<tt:PTZ>"),
		}
		if info, err := c.DeviceInfo(ctx); err == nil {
			res.Device = info
		} else if err == ErrUnauthorized {
			// The service is there but these credentials can't read it — that is
			// the useful thing to say, not "no ONVIF here".
			res.Error = "the camera refused these ONVIF credentials"
		}
		if tok, err := c.VideoSourceToken(ctx); err == nil {
			res.VideoSource = tok
		}
		return res
	}
	return ProbeResult{Error: firstNonEmpty(lastErr, "no ONVIF service answered on this camera")}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// DeviceInfo reads the camera's make/model/firmware.
func (c *Client) DeviceInfo(ctx context.Context) (DeviceInfo, error) {
	out, err := c.call(ctx, "device_service", `<tds:GetDeviceInformation/>`)
	if err != nil {
		return DeviceInfo{}, err
	}
	return DeviceInfo{
		Manufacturer: innerText(out, "Manufacturer"),
		Model:        innerText(out, "Model"),
		Firmware:     innerText(out, "FirmwareVersion"),
		Serial:       innerText(out, "SerialNumber"),
	}, nil
}

// VideoSourceToken finds the token every imaging call is addressed to.
func (c *Client) VideoSourceToken(ctx context.Context) (string, error) {
	out, err := c.call(ctx, "media_service", `<trt:GetVideoSources/>`)
	if err != nil {
		return "", err
	}
	// The token is an attribute on VideoSources; grab the first one.
	if i := strings.Index(out, `token="`); i >= 0 {
		rest := out[i+len(`token="`):]
		if j := strings.Index(rest, `"`); j > 0 {
			return rest[:j], nil
		}
	}
	return "", fmt.Errorf("the camera did not report a video source")
}

// Imaging reads current settings plus the ranges the camera allows.
func (c *Client) Imaging(ctx context.Context, videoSource string) (*Imaging, error) {
	cur, err := c.call(ctx, "imaging_service",
		`<timg:GetImagingSettings><timg:VideoSourceToken>`+
			xmlEscape(videoSource)+`</timg:VideoSourceToken></timg:GetImagingSettings>`)
	if err != nil {
		return nil, err
	}
	im := &Imaging{Ranges: map[string]Range{}}
	im.Brightness = floatOrNil(innerText(cur, "Brightness"))
	im.Contrast = floatOrNil(innerText(cur, "Contrast"))
	im.Saturation = floatOrNil(innerText(cur, "ColorSaturation"))
	im.Sharpness = floatOrNil(innerText(cur, "Sharpness"))
	im.IrCutFilter = innerText(cur, "IrCutFilter")

	// Options are advisory: a camera that won't report ranges still gets sliders,
	// using its own values as the bound hint.
	if opts, err := c.call(ctx, "imaging_service",
		`<timg:GetOptions><timg:VideoSourceToken>`+
			xmlEscape(videoSource)+`</timg:VideoSourceToken></timg:GetOptions>`); err == nil {
		for name, key := range map[string]string{
			"Brightness": "brightness", "Contrast": "contrast",
			"ColorSaturation": "saturation", "Sharpness": "sharpness",
		} {
			if r, ok := rangeOf(opts, name); ok {
				im.Ranges[key] = r
			}
		}
		im.IrCutOptions = uniq(allValues(opts, "Mode"))
	}
	return im, nil
}

// rangeOf extracts <Name><Min>..</Min><Max>..</Max></Name> for one setting.
func rangeOf(doc, name string) (Range, bool) {
	i := strings.Index(doc, name+">")
	if i < 0 {
		return Range{}, false
	}
	seg := doc[i:]
	if e := strings.Index(seg, "</"); e > 0 && e < len(seg) {
		if end := strings.Index(seg, "Max>"); end > 0 {
			seg = seg[:min(len(seg), end+40)]
		}
	}
	minV := floatOrNil(innerText(seg, "Min"))
	maxV := floatOrNil(innerText(seg, "Max"))
	if minV == nil || maxV == nil || *maxV <= *minV {
		return Range{}, false
	}
	return Range{Min: *minV, Max: *maxV}, true
}

// SetImaging applies changed values. ONVIF wants the full settings block in
// schema order, so unchanged values are sent back as-is (cur) alongside the new
// ones — a partial block is rejected by most firmware.
func (c *Client) SetImaging(ctx context.Context, videoSource string, cur, next *Imaging) error {
	pick := func(a, b *float64) *float64 {
		if a != nil {
			return a
		}
		return b
	}
	b := &strings.Builder{}
	b.WriteString(`<timg:SetImagingSettings><timg:VideoSourceToken>`)
	b.WriteString(xmlEscape(videoSource))
	b.WriteString(`</timg:VideoSourceToken><timg:ImagingSettings>`)
	writeNum := func(tag string, v *float64) {
		if v != nil {
			fmt.Fprintf(b, "<tt:%s>%s</tt:%s>", tag, strconv.FormatFloat(*v, 'f', -1, 64), tag)
		}
	}
	// Schema sequence order matters to strict firmware.
	writeNum("Brightness", pick(next.Brightness, cur.Brightness))
	writeNum("ColorSaturation", pick(next.Saturation, cur.Saturation))
	writeNum("Contrast", pick(next.Contrast, cur.Contrast))
	ir := next.IrCutFilter
	if ir == "" {
		ir = cur.IrCutFilter
	}
	if ir != "" {
		b.WriteString("<tt:IrCutFilter>" + xmlEscape(ir) + "</tt:IrCutFilter>")
	}
	writeNum("Sharpness", pick(next.Sharpness, cur.Sharpness))
	b.WriteString(`</timg:ImagingSettings><timg:ForcePersistence>true</timg:ForcePersistence></timg:SetImagingSettings>`)

	_, err := c.call(ctx, "imaging_service", b.String())
	return err
}

func floatOrNil(s string) *float64 {
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &f
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
