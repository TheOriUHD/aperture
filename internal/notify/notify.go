// Package notify evaluates user notification rules against events and fans the
// matches out to delivery channels (Web Push, ntfy, Telegram, Discord, generic
// webhook, email). Event-gated, ACL-aware, with quiet hours and per-rule
// cooldown.
package notify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/TheOriUHD/aperture/internal/astro"
	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/site"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Engine is the notification dispatcher.
type Engine struct {
	cfg       *config.Config
	st        *store.Store
	log       *slog.Logger
	vapidPub  string
	vapidPriv string
}

// New builds the engine and ensures a VAPID keypair exists (persisted in
// settings) for Web Push.
func New(cfg *config.Config, st *store.Store, log *slog.Logger) *Engine {
	e := &Engine{cfg: cfg, st: st, log: log}
	pub, _ := st.GetSetting("vapid_public")
	priv, _ := st.GetSetting("vapid_private")
	if pub == "" || priv == "" {
		if p, q, err := webpush.GenerateVAPIDKeys(); err == nil {
			priv, pub = p, q
			_ = st.SetSetting("vapid_public", pub)
			_ = st.SetSetting("vapid_private", priv)
		} else {
			log.Error("notify: generate VAPID keys", "err", err)
		}
	}
	e.vapidPub, e.vapidPriv = pub, priv
	return e
}

// VAPIDPublic is the public key the browser needs to subscribe.
func (e *Engine) VAPIDPublic() string { return e.vapidPub }

// Note is a channel-agnostic notification payload.
type Note struct {
	Title string
	Body  string
	URL   string
	Image string
}

// OnEvent is the hook the motion detector calls when a new event opens.
func (e *Engine) OnEvent(ev *store.Event) {
	cam, err := e.st.GetCamera(ev.CameraID)
	if err != nil {
		return
	}
	rules, err := e.st.ListEnabledRules()
	if err != nil {
		return
	}
	now := time.Now()
	ec := e.eventContext(ev)
	for _, r := range rules {
		if !match(r.Cameras, ev.CameraID) || !match(r.EventTypes, ev.Type) {
			continue
		}
		if !conditionsMet(r.Conditions, ec) {
			continue
		}
		if inQuietHours(r, now) {
			continue
		}
		if !e.userAllowed(r.UserID, ev.CameraID) {
			continue
		}
		if r.CooldownS > 0 {
			if last, _ := e.st.LastNotify(r.ID); last > 0 && now.UnixMilli()-last < int64(r.CooldownS)*1000 {
				continue
			}
		}
		_ = e.st.RecordNotify(r.ID, ev.ID, now.UnixMilli())
		note := e.buildNote(ev, cam)
		go e.fire(r, note)
	}
}

// evContext is the environmental context at an event, computed once per event.
type evContext struct {
	configured bool
	isNight    bool
	precip     bool
}

// eventContext joins the event time against the context layer (weather + astro).
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

// conditionsMet reports whether a rule's environmental gates pass. When no site
// is configured the gates can't be evaluated, so we don't suppress.
func conditionsMet(c store.RuleConditions, ec evContext) bool {
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

func (e *Engine) buildNote(ev *store.Event, cam *store.Camera) Note {
	title := "Motion detected"
	if ev.Type != "motion" && ev.Type != "" {
		title = strings.ToUpper(ev.Type[:1]) + ev.Type[1:]
	}
	return Note{
		Title: title + " · " + cam.Name,
		Body:  time.UnixMilli(ev.StartTS).Format("Mon 15:04:05"),
		URL:   e.deepLink(cam.ID, ev.StartTS),
		Image: e.ThumbURL(ev.ID),
	}
}

func (e *Engine) fire(r *store.NotifyRule, n Note) {
	for _, chID := range r.Channels {
		ch, err := e.st.GetChannel(chID)
		if err != nil || !ch.Enabled {
			continue
		}
		if err := e.send(ch, n); err != nil {
			e.log.Warn("notify: channel send failed", "type", ch.Type, "err", err)
		}
	}
}

// TestSend delivers a sample notification through one channel.
func (e *Engine) TestSend(ch *store.NotifyChannel) error {
	return e.send(ch, Note{
		Title: "Aperture test notification",
		Body:  "If you can read this, " + ch.Name + " is working.",
		URL:   strings.TrimRight(e.cfg.PublicURL, "/") + "/dashboard",
	})
}

func (e *Engine) userAllowed(userID, cameraID string) bool {
	u, err := e.st.GetUserByID(userID)
	if err != nil {
		return false
	}
	if u.Role == store.RoleAdmin {
		return true
	}
	ids, _ := e.st.GetUserCameraIDs(userID)
	for _, id := range ids {
		if id == cameraID {
			return true
		}
	}
	return false
}

func (e *Engine) deepLink(camID string, ts int64) string {
	return fmt.Sprintf("%s/playback?camera=%s&t=%d", strings.TrimRight(e.cfg.PublicURL, "/"),
		url.QueryEscape(camID), ts)
}

// ThumbURL returns a short-lived signed public URL for an event thumbnail, so
// external channels (Telegram/Discord/email) can show the image off-LAN.
func (e *Engine) ThumbURL(eventID string) string {
	exp := time.Now().Add(24 * time.Hour).Unix()
	return fmt.Sprintf("%s/api/public/event-thumb?e=%s&exp=%d&sig=%s",
		strings.TrimRight(e.cfg.PublicURL, "/"), url.QueryEscape(eventID), exp,
		signThumb(e.cfg.StreamSecret, eventID, exp))
}

func signThumb(secret []byte, eventID string, exp int64) string {
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "%s.%d", eventID, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyThumb checks a signed thumbnail URL (used by the public API handler).
func VerifyThumb(secret []byte, eventID, sig string, exp int64) bool {
	if time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(signThumb(secret, eventID, exp)))
}

func match(set []string, v string) bool {
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

func inQuietHours(r *store.NotifyRule, now time.Time) bool {
	if r.QuietStart < 0 || r.QuietEnd < 0 {
		return false
	}
	m := now.Hour()*60 + now.Minute()
	if r.QuietStart <= r.QuietEnd {
		return m >= r.QuietStart && m < r.QuietEnd
	}
	return m >= r.QuietStart || m < r.QuietEnd // wraps midnight
}
