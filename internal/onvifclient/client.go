// Package onvifclient talks TO a camera's own ONVIF service, so Aperture can
// read and adjust the settings the camera exposes (brightness, contrast,
// saturation, sharpness, day/night IR-cut) without opening the vendor's web UI.
//
// This is the opposite direction to internal/onvif, which SERVES ONVIF to
// external systems. Nothing here touches the recording path: every call is a
// short-timeout HTTP request made on demand, and any failure is reported rather
// than retried in a way that could stall a request.
package onvifclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultPorts are the ONVIF service ports cameras commonly use. Vendors differ
// (Reolink 8000, many others 2020 or 80), so a probe walks this list rather than
// making the operator guess.
var DefaultPorts = []int{8000, 2020, 80, 8899, 8080, 5000}

// Client addresses one camera's ONVIF services.
type Client struct {
	Host string // host:port
	User string
	Pass string
	HTTP *http.Client
}

// New builds a client with a bounded HTTP timeout — a camera that hangs must
// never hold a request open.
func New(host, user, pass string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Client{Host: host, User: user, Pass: pass, HTTP: &http.Client{Timeout: timeout}}
}

// wsSecurity builds the WS-UsernameToken header ONVIF expects:
// PasswordDigest = base64(sha1(nonce + created + password)).
func (c *Client) wsSecurity() string {
	if c.User == "" {
		return ""
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	created := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	h := sha1.New()
	h.Write(nonce)
	h.Write([]byte(created))
	h.Write([]byte(c.Pass))
	digest := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return `<s:Header><Security s:mustUnderstand="1" xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd">` +
		`<UsernameToken><Username>` + xmlEscape(c.User) + `</Username>` +
		`<Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">` + digest + `</Password>` +
		`<Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">` +
		base64.StdEncoding.EncodeToString(nonce) + `</Nonce>` +
		`<Created xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">` + created + `</Created>` +
		`</UsernameToken></Security></s:Header>`
}

// ErrUnauthorized means the camera accepted the credentials but refuses this
// operation for that account — typical for a viewer-level ONVIF user, which can
// read settings but not change them.
var ErrUnauthorized = fmt.Errorf("the camera rejected this account for that action")

// call posts a SOAP body to one of the camera's services and returns the body.
func (c *Client) call(ctx context.Context, service, body string) (string, error) {
	env := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">` +
		c.wsSecurity() + `<s:Body>` + body + `</s:Body></s:Envelope>`
	u := "http://" + c.Host + "/onvif/" + service
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(env))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	out := string(raw)
	if res.StatusCode >= 400 || strings.Contains(out, ":Fault") {
		reason := faultReason(out)
		if strings.Contains(strings.ToLower(reason), "not authorized") ||
			strings.Contains(strings.ToLower(reason), "authorization") {
			return out, ErrUnauthorized
		}
		if reason == "" {
			reason = fmt.Sprintf("the camera returned HTTP %d", res.StatusCode)
		}
		return out, fmt.Errorf("%s", reason)
	}
	return out, nil
}

func faultReason(s string) string {
	if v := innerText(s, "Text"); v != "" {
		return v
	}
	return innerText(s, "Reason")
}

// innerText pulls the first <...:local>…</...:local> value, namespace-agnostic —
// vendors differ wildly in prefixes, so matching on the local name is the only
// thing that works across them.
func innerText(doc, local string) string {
	dec := xml.NewDecoder(strings.NewReader(doc))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == local {
			var b strings.Builder
			for {
				t2, err := dec.Token()
				if err != nil {
					return strings.TrimSpace(b.String())
				}
				switch v := t2.(type) {
				case xml.CharData:
					b.Write(v)
				case xml.EndElement:
					if v.Name.Local == local {
						return strings.TrimSpace(b.String())
					}
				}
			}
		}
	}
}

// allValues returns every occurrence of a local element name.
func allValues(doc, local string) []string {
	var out []string
	dec := xml.NewDecoder(strings.NewReader(doc))
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == local {
			var b strings.Builder
			depth := 0
			for {
				t2, err := dec.Token()
				if err != nil {
					return out
				}
				switch v := t2.(type) {
				case xml.CharData:
					if depth == 0 {
						b.Write(v)
					}
				case xml.StartElement:
					depth++
				case xml.EndElement:
					if v.Name.Local == local && depth == 0 {
						out = append(out, strings.TrimSpace(b.String()))
						goto next
					}
					depth--
				}
			}
		}
	next:
	}
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// HostFromRTSP extracts the bare host (no port) and any credentials embedded in
// an RTSP URL, so ONVIF can default to the same camera and login the operator
// already entered for streaming.
func HostFromRTSP(rtsp string) (host, user, pass string) {
	u, err := url.Parse(rtsp)
	if err != nil {
		return "", "", ""
	}
	host = u.Hostname()
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	return host, user, pass
}

// reachable reports whether a TCP connect to host:port succeeds quickly.
func reachable(ctx context.Context, host string, port int, d time.Duration) bool {
	dialer := net.Dialer{Timeout: d}
	c, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
