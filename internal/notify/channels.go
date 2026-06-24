package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/TheOriUHD/aperture/internal/store"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

func cfgMap(s string) map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

func (e *Engine) send(ch *store.NotifyChannel, n Note) error {
	c := cfgMap(ch.Config)
	switch ch.Type {
	case "webpush":
		return e.sendWebPush(ch.UserID, n)
	case "ntfy":
		return sendNtfy(c, n)
	case "telegram":
		return sendTelegram(c, n)
	case "discord":
		return sendDiscord(c, n)
	case "webhook":
		return sendWebhook(c, n)
	case "email":
		return sendEmail(c, n)
	}
	return fmt.Errorf("unknown channel type %q", ch.Type)
}

func (e *Engine) sendWebPush(userID string, n Note) error {
	subs, err := e.st.ListPushSubsByUser(userID)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return fmt.Errorf("no registered devices for push")
	}
	payload, _ := json.Marshal(map[string]string{"title": n.Title, "body": n.Body, "url": n.URL, "image": n.Image})
	var firstErr error
	for _, s := range subs {
		sub := &webpush.Subscription{Endpoint: s.Endpoint, Keys: webpush.Keys{P256dh: s.P256dh, Auth: s.Auth}}
		resp, err := webpush.SendNotification(payload, sub, &webpush.Options{
			Subscriber:      "mailto:aperture@localhost",
			VAPIDPublicKey:  e.vapidPub,
			VAPIDPrivateKey: e.vapidPriv,
			TTL:             120,
		})
		if err != nil {
			firstErr = err
			continue
		}
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			_ = e.st.DeletePushSubByEndpoint(s.Endpoint)
		}
		resp.Body.Close()
	}
	return firstErr
}

func sendNtfy(c map[string]string, n Note) error {
	topic := c["topic"]
	if topic == "" {
		return fmt.Errorf("ntfy topic required")
	}
	server := strings.TrimRight(def(c["server"], "https://ntfy.sh"), "/")
	req, _ := http.NewRequest(http.MethodPost, server+"/"+topic, strings.NewReader(n.Body))
	req.Header.Set("Title", n.Title)
	if n.URL != "" {
		req.Header.Set("Click", n.URL)
	}
	if n.Image != "" {
		req.Header.Set("Attach", n.Image)
	}
	if c["token"] != "" {
		req.Header.Set("Authorization", "Bearer "+c["token"])
	}
	return doReq(req)
}

func sendTelegram(c map[string]string, n Note) error {
	token, chat := c["token"], c["chatId"]
	if token == "" || chat == "" {
		return fmt.Errorf("telegram token and chatId required")
	}
	text := "*" + tgEscape(n.Title) + "*\n" + tgEscape(n.Body)
	if n.URL != "" {
		text += "\n" + n.URL
	}
	form := url.Values{"chat_id": {chat}, "parse_mode": {"Markdown"}}
	var api string
	if n.Image != "" {
		api = fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", token)
		form.Set("photo", n.Image)
		form.Set("caption", text)
	} else {
		api = fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
		form.Set("text", text)
	}
	resp, err := httpClient.PostForm(api, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram: %s", resp.Status)
	}
	return nil
}

func sendDiscord(c map[string]string, n Note) error {
	wh := c["webhookUrl"]
	if wh == "" {
		return fmt.Errorf("discord webhookUrl required")
	}
	embed := map[string]any{"title": n.Title, "description": n.Body}
	if n.URL != "" {
		embed["url"] = n.URL
	}
	if n.Image != "" {
		embed["image"] = map[string]string{"url": n.Image}
	}
	body, _ := json.Marshal(map[string]any{"embeds": []any{embed}})
	req, _ := http.NewRequest(http.MethodPost, wh, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return doReq(req)
}

func sendWebhook(c map[string]string, n Note) error {
	target := c["url"]
	if target == "" {
		return fmt.Errorf("webhook url required")
	}
	body, _ := json.Marshal(map[string]string{"title": n.Title, "body": n.Body, "url": n.URL, "image": n.Image})
	req, _ := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return doReq(req)
}

func sendEmail(c map[string]string, n Note) error {
	host, from, to := c["host"], c["from"], c["to"]
	if host == "" || from == "" || to == "" {
		return fmt.Errorf("email host/from/to required")
	}
	img := ""
	if n.Image != "" {
		img = fmt.Sprintf(`<p><img src="%s" style="max-width:480px;border-radius:8px"></p>`, n.Image)
	}
	html := fmt.Sprintf(`<html><body style="font-family:sans-serif"><h2>%s</h2><p>%s</p>%s<p><a href="%s">Open in Aperture</a></p></body></html>`,
		n.Title, n.Body, img, n.URL)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		from, to, n.Title, html)
	var auth smtp.Auth
	if c["username"] != "" {
		auth = smtp.PlainAuth("", c["username"], c["password"], host)
	}
	return smtp.SendMail(host+":"+def(c["port"], "587"), auth, from, strings.Split(to, ","), []byte(msg))
}

func doReq(req *http.Request) error {
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	return nil
}

func def(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func tgEscape(s string) string {
	return strings.NewReplacer("_", "\\_", "*", "\\*", "[", "\\[", "`", "\\`").Replace(s)
}
