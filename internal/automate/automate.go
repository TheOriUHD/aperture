// Package automate is Aperture's local when-this-then-that engine. Events flow
// through it; matching automations fire actions that dispatch to existing
// subsystems (notifications, webhooks, MQTT, the recorder). It reuses the same
// predicate ideas as notification rules and generalizes them to actions.
package automate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/TheOriUHD/aperture/internal/astro"
	"github.com/TheOriUHD/aperture/internal/site"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Trigger selects which events start an automation. Empty fields mean "any".
type Trigger struct {
	EventTypes []string `json:"eventTypes"`
	Objects    []string `json:"objects"` // require one of these detected labels
	Cameras    []string `json:"cameras"`
}

// Conditions gate on the environmental context at the event.
type Conditions struct {
	TimeOfDay  string `json:"timeOfDay"`  // "" | "day" | "night"
	OnlyPrecip bool   `json:"onlyPrecip"` // only when precipitation > 0
}

// Action is one thing an automation does when it fires.
type Action struct {
	Type       string   `json:"type"`       // notify | webhook | mqtt | record_mode
	Channels   []string `json:"channels"`   // notify: notification-channel ids
	URL        string   `json:"url"`        // webhook: target URL
	Topic      string   `json:"topic"`      // mqtt: topic
	Payload    string   `json:"payload"`    // mqtt: payload
	Camera     string   `json:"camera"`     // record_mode: target camera id
	RecordMode string   `json:"recordMode"` // record_mode: never|continuous|motion
}

// Notifier sends a note to specific notification channels.
type Notifier interface {
	SendNote(channelIDs []string, title, body, url string)
}

// MQTTPublisher publishes a raw payload to a topic (the HA link).
type MQTTPublisher interface {
	PublishRaw(topic string, payload []byte)
}

// Engine evaluates automations against events and runs their actions.
type Engine struct {
	st        *store.Store
	notify    Notifier
	mqtt      MQTTPublisher
	reconcile func() // re-sync recorder + detector after a record-mode change
	publicURL string
	http      *http.Client
	log       *slog.Logger
}

func New(st *store.Store, notify Notifier, mqtt MQTTPublisher, reconcile func(), publicURL string, log *slog.Logger) *Engine {
	return &Engine{
		st: st, notify: notify, mqtt: mqtt, reconcile: reconcile,
		publicURL: strings.TrimRight(publicURL, "/"),
		http:      &http.Client{Timeout: 10 * time.Second},
		log:       log,
	}
}

// OnEvent is the hook the event pipeline calls for every new event.
func (e *Engine) OnEvent(ev *store.Event) {
	autos, err := e.st.ListEnabledAutomations()
	if err != nil || len(autos) == 0 {
		return
	}
	now := time.Now()
	ec := e.eventContext(ev)
	for _, a := range autos {
		var trig Trigger
		_ = json.Unmarshal(a.Trigger, &trig)
		if !triggerMatches(trig, ev) {
			continue
		}
		var cond Conditions
		_ = json.Unmarshal(a.Conditions, &cond)
		if !conditionsMet(cond, ec) {
			continue
		}
		if a.CooldownMs > 0 && a.LastFired > 0 && now.UnixMilli()-a.LastFired < a.CooldownMs {
			continue
		}
		e.st.TouchAutomationFired(a.ID, now.UnixMilli())
		e.log.Info("automation fired", "name", a.Name, "event", ev.ID, "camera", ev.CameraID)
		e.Run(a.Actions, ev)
	}
}

// Run executes an automation's actions against an event (each async).
func (e *Engine) Run(actionsJSON json.RawMessage, ev *store.Event) {
	var actions []Action
	if err := json.Unmarshal(actionsJSON, &actions); err != nil {
		return
	}
	for _, act := range actions {
		a := act
		go e.runAction(a, ev)
	}
}

func (e *Engine) runAction(a Action, ev *store.Event) {
	switch a.Type {
	case "notify":
		if e.notify != nil {
			title := "Automation"
			if ev.ObjectSummary != "" {
				title = strings.ToUpper(ev.ObjectSummary[:1]) + ev.ObjectSummary[1:]
			} else if ev.Type != "" {
				title = strings.ToUpper(ev.Type[:1]) + ev.Type[1:]
			}
			body := time.UnixMilli(ev.StartTS).Format("Mon 15:04:05")
			e.notify.SendNote(a.Channels, title, body, e.deepLink(ev))
		}
	case "webhook":
		if a.URL != "" {
			e.postWebhook(a.URL, ev)
		}
	case "mqtt":
		if e.mqtt != nil && a.Topic != "" {
			e.mqtt.PublishRaw(a.Topic, []byte(a.Payload))
		}
	case "record_mode":
		e.setRecordMode(a.Camera, a.RecordMode)
	default:
		e.log.Warn("automation: unknown action type", "type", a.Type)
	}
}

func (e *Engine) setRecordMode(cameraID, mode string) {
	if mode != store.ModeNever && mode != store.ModeContinuous && mode != store.ModeMotion {
		return
	}
	cam, err := e.st.GetCamera(cameraID)
	if err != nil {
		return
	}
	if cam.RecordMode == mode {
		return
	}
	cam.RecordMode = mode
	if err := e.st.UpdateCamera(cam); err != nil {
		e.log.Warn("automation: set record mode", "err", err)
		return
	}
	if e.reconcile != nil {
		e.reconcile() // apply immediately (recorder + detector) instead of waiting for the periodic sync
	}
}

func (e *Engine) postWebhook(url string, ev *store.Event) {
	body, _ := json.Marshal(map[string]any{
		"type": "automation", "ts": time.Now().UnixMilli(),
		"event": map[string]any{
			"id": ev.ID, "cameraId": ev.CameraID, "eventType": ev.Type,
			"startTs": ev.StartTS, "topLabel": ev.TopLabel,
		},
		"link": e.deepLink(ev),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.http.Do(req)
	if err != nil {
		e.log.Warn("automation: webhook post", "err", err)
		return
	}
	_ = resp.Body.Close()
}

func (e *Engine) deepLink(ev *store.Event) string {
	if e.publicURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/playback?camera=%s&t=%d", e.publicURL, ev.CameraID, ev.StartTS)
}

func triggerMatches(t Trigger, ev *store.Event) bool {
	if !anyOrEmpty(t.EventTypes, ev.Type) {
		return false
	}
	if !anyOrEmpty(t.Cameras, ev.CameraID) {
		return false
	}
	if len(t.Objects) > 0 {
		hit := false
		for _, want := range t.Objects {
			for _, have := range ev.Labels {
				if want == have {
					hit = true
				}
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

func anyOrEmpty(set []string, v string) bool {
	if len(set) == 0 {
		return true
	}
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// ---- environmental context (mirrors the notify engine) --------------------

type evContext struct {
	configured bool
	isNight    bool
	precip     bool
}

func (e *Engine) eventContext(ev *store.Event) evContext {
	loc := site.Load(e.st)
	if !loc.Configured {
		return evContext{}
	}
	ec := evContext{configured: true}
	tsSec := ev.StartTS / 1000
	if ws, err := e.st.WeatherAt(tsSec); err == nil && ws != nil && ws.PrecipMm != nil {
		ec.precip = *ws.PrecipMm > 0
	}
	tz, err := time.LoadLocation(loc.Timezone)
	if err != nil {
		tz = time.UTC
	}
	t := time.Unix(tsSec, 0).In(tz)
	noon := time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, tz)
	sun := astro.Sun(noon, loc.Lat, loc.Lon)
	ec.isNight = sun.Sunrise > 0 && sun.Sunset > 0 && (tsSec < sun.Sunrise || tsSec > sun.Sunset)
	return ec
}

func conditionsMet(c Conditions, ec evContext) bool {
	if (c.TimeOfDay == "" && !c.OnlyPrecip) || !ec.configured {
		return true
	}
	if c.TimeOfDay == "day" && ec.isNight {
		return false
	}
	if c.TimeOfDay == "night" && !ec.isNight {
		return false
	}
	if c.OnlyPrecip && !ec.precip {
		return false
	}
	return true
}
