// Package webhook delivers events to operator-configured outbound HTTP
// endpoints (n8n, Node-RED, custom services) as signed JSON, with retry and a
// delivery log. Webhooks are admin-configured, so the target URLs are trusted.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// Dispatcher fans events out to matching webhooks.
type Dispatcher struct {
	st        *store.Store
	log       *slog.Logger
	http      *http.Client
	publicURL string
}

func New(st *store.Store, publicURL string, log *slog.Logger) *Dispatcher {
	return &Dispatcher{
		st: st, log: log, publicURL: strings.TrimRight(publicURL, "/"),
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// OnEvent fires every enabled webhook whose event filter matches, off the hot
// path (each delivery in its own goroutine, with retry).
func (d *Dispatcher) OnEvent(ev *store.Event) {
	whs, err := d.st.ListEnabledWebhooks(ev.Type)
	if err != nil || len(whs) == 0 {
		return
	}
	body, err := json.Marshal(d.eventPayload(ev))
	if err != nil {
		return
	}
	for _, wh := range whs {
		go d.deliver(wh, ev.Type, body)
	}
}

func (d *Dispatcher) eventPayload(ev *store.Event) map[string]any {
	ev0 := map[string]any{
		"id": ev.ID, "cameraId": ev.CameraID, "eventType": ev.Type,
		"startTs": ev.StartTS, "endTs": ev.EndTS, "score": ev.Score,
	}
	if ev.TopLabel != "" {
		ev0["topLabel"] = ev.TopLabel
		ev0["objects"] = ev.Labels
	}
	p := map[string]any{"type": "event", "ts": time.Now().UnixMilli(), "event": ev0}
	if d.publicURL != "" {
		p["link"] = fmt.Sprintf("%s/playback?camera=%s&t=%d", d.publicURL, url.QueryEscape(ev.CameraID), ev.StartTS)
		p["thumbUrl"] = fmt.Sprintf("%s/api/events/%s/thumb.jpg", d.publicURL, url.QueryEscape(ev.ID))
	}
	return p
}

func (d *Dispatcher) deliver(wh store.Webhook, eventType string, body []byte) {
	var detail string
	var code int
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		c, err := d.post(wh, body)
		code = c
		if err == nil && c >= 200 && c < 300 {
			d.record(wh.ID, eventType, "ok", c, "")
			return
		}
		if err != nil {
			detail = err.Error()
		} else {
			detail = fmt.Sprintf("HTTP %d", c)
		}
	}
	d.log.Warn("webhook: delivery failed", "webhook", wh.Name, "err", detail)
	d.record(wh.ID, eventType, "failed", code, detail)
}

func (d *Dispatcher) post(wh store.Webhook, body []byte) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Aperture-Webhook/1")
	if wh.Secret != "" {
		req.Header.Set("X-Aperture-Signature", "sha256="+sign(wh.Secret, body))
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, nil
}

func (d *Dispatcher) record(webhookID, eventType, status string, code int, detail string) {
	d.st.RecordDelivery(store.WebhookDelivery{
		WebhookID: webhookID, Event: eventType, Status: status, Code: code, Detail: detail,
		TS: time.Now().UnixMilli(),
	})
}

// TestFire delivers a sample payload to a webhook (for the Settings "Test"
// button) and returns the HTTP status.
func (d *Dispatcher) TestFire(wh *store.Webhook) (int, error) {
	body, _ := json.Marshal(map[string]any{
		"type": "test", "message": "Aperture webhook test", "ts": time.Now().UnixMilli(),
	})
	return d.post(*wh, body)
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
