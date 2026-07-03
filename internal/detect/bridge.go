package detect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder for DecodeConfig
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

var errUnavailable = errors.New("detection backend unavailable")

// bridgeBackend posts candidate frames to an external inference server and
// ingests the results. It speaks the DeepStack / CodeProject.AI detection API
// (the de-facto standard many self-hosters already run for Frigate etc.), plus
// a "generic" mode that expects normalized boxes. Pure HTTP — no native deps.
type bridgeBackend struct {
	url  string
	api  string
	http *http.Client
}

func newBridge(c Config) Backend {
	return &bridgeBackend{
		url:  c.BridgeURL,
		api:  c.BridgeAPI,
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

func (b *bridgeBackend) Name() string { return "bridge(" + b.api + ")" }
func (b *bridgeBackend) Close()       {}

func (b *bridgeBackend) Detect(ctx context.Context, jpeg []byte) ([]Detection, error) {
	if b.url == "" {
		return nil, errUnavailable
	}
	// Image dimensions let us normalize pixel boxes to 0..1.
	iw, ih := 0, 0
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(jpeg)); err == nil {
		iw, ih = cfg.Width, cfg.Height
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("image", "frame.jpg")
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(jpeg); err != nil {
		return nil, err
	}
	_ = mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.url, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("detector %s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if b.api == "generic" {
		return parseGeneric(raw)
	}
	return parseDeepStack(raw, iw, ih)
}

// deepStackResp matches DeepStack / CodeProject.AI vision detection.
type deepStackResp struct {
	Success     bool `json:"success"`
	Predictions []struct {
		Label      string  `json:"label"`
		Confidence float64 `json:"confidence"`
		XMin       float64 `json:"x_min"`
		YMin       float64 `json:"y_min"`
		XMax       float64 `json:"x_max"`
		YMax       float64 `json:"y_max"`
	} `json:"predictions"`
	Error string `json:"error"`
}

func parseDeepStack(raw []byte, iw, ih int) ([]Detection, error) {
	var r deepStackResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("decode detector response: %w", err)
	}
	if r.Error != "" {
		return nil, errors.New(r.Error)
	}
	if iw <= 0 || ih <= 0 {
		iw, ih = 1, 1 // avoid div-by-zero; boxes will be degenerate but labels still work
	}
	out := make([]Detection, 0, len(r.Predictions))
	for _, p := range r.Predictions {
		fw := float64(iw)
		fh := float64(ih)
		x := clamp01(p.XMin / fw)
		y := clamp01(p.YMin / fh)
		out = append(out, Detection{
			Label: p.Label, Confidence: p.Confidence,
			X: x, Y: y,
			W: clamp01(p.XMax/fw) - x,
			H: clamp01(p.YMax/fh) - y,
		})
	}
	return out, nil
}

// genericResp is Aperture's own simple schema: already-normalized boxes.
type genericResp struct {
	Detections []Detection `json:"detections"`
}

func parseGeneric(raw []byte) ([]Detection, error) {
	var r genericResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("decode detector response: %w", err)
	}
	return r.Detections, nil
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
