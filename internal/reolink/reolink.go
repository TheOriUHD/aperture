// Package reolink talks to a Reolink camera's own HTTP API — the one its web UI
// and app use — for controls ONVIF does not model at all, most usefully the LED
// spotlight and the IR illuminators.
//
// Vendor-specific by necessity: there is no standard way to ask a camera to turn
// its floodlight on. It is only ever used for cameras that ONVIF already
// identified as Reolink, and every call is short-timeout and best-effort, so a
// camera that stops answering degrades to a message in one panel.
package reolink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Client holds a camera's HTTP endpoint and a cached login token. Reolink issues
// a token with a lease (an hour on current firmware); it is reused until shortly
// before expiry rather than logging in on every call.
type Client struct {
	Host string // host or host:port (defaults to :80)
	User string
	Pass string
	HTTP *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

func New(host, user, pass string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 6 * time.Second
	}
	return &Client{
		Host: host, User: user, Pass: pass,
		HTTP: &http.Client{Timeout: timeout, Transport: &http.Transport{DisableKeepAlives: true}},
	}
}

type apiResp struct {
	Cmd   string          `json:"cmd"`
	Code  int             `json:"code"`
	Value json.RawMessage `json:"value"`
	Error *struct {
		Detail  string `json:"detail"`
		RspCode int    `json:"rspCode"`
	} `json:"error"`
}

func (c *Client) post(ctx context.Context, query string, payload any) ([]apiResp, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := "http://" + c.Host + "/cgi-bin/api.cgi?" + query
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Close = true
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var out []apiResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("the camera returned an unexpected response")
	}
	if len(out) > 0 && out[0].Error != nil {
		return out, fmt.Errorf("%s", out[0].Error.Detail)
	}
	return out, nil
}

// login fetches (and caches) a token.
func (c *Client) login(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.expires) {
		t := c.token
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()

	res, err := c.post(ctx, "cmd=Login", []map[string]any{{
		"cmd": "Login",
		"param": map[string]any{"User": map[string]string{
			"userName": c.User, "password": c.Pass,
		}},
	}})
	if err != nil {
		return "", err
	}
	var v struct {
		Token struct {
			Name      string `json:"name"`
			LeaseTime int    `json:"leaseTime"`
		} `json:"Token"`
	}
	if len(res) == 0 || json.Unmarshal(res[0].Value, &v) != nil || v.Token.Name == "" {
		return "", fmt.Errorf("the camera did not accept these credentials")
	}
	lease := time.Duration(v.Token.LeaseTime) * time.Second
	if lease <= 0 {
		lease = time.Hour
	}
	c.mu.Lock()
	c.token = v.Token.Name
	// Renew a minute early so a call never races the expiry.
	c.expires = time.Now().Add(lease - time.Minute)
	c.mu.Unlock()
	return v.Token.Name, nil
}

// call runs one authenticated command, retrying once if the token was rejected
// (a camera reboot invalidates it and the cached one would otherwise stick).
func (c *Client) call(ctx context.Context, cmd string, action int, param any, out any) error {
	run := func() ([]apiResp, error) {
		tok, err := c.login(ctx)
		if err != nil {
			return nil, err
		}
		return c.post(ctx, "cmd="+cmd+"&token="+tok, []map[string]any{{
			"cmd": cmd, "action": action, "param": param,
		}})
	}
	res, err := run()
	if err != nil {
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		if res, err = run(); err != nil {
			return err
		}
	}
	if out != nil && len(res) > 0 && len(res[0].Value) > 0 {
		return json.Unmarshal(res[0].Value, out)
	}
	return nil
}

// Spotlight is the white LED floodlight.
//
// mode: 0 off · 1 night · 2 on motion · 3 scheduled — firmware-dependent, so the
// UI offers On/Off/Auto and leaves richer scheduling to the camera's own app.
type Spotlight struct {
	State  int `json:"state"`  // 0/1
	Mode   int `json:"mode"`   // see above
	Bright int `json:"bright"` // 0..100
}

type whiteLedWrap struct {
	WhiteLed struct {
		State   int `json:"state"`
		Mode    int `json:"mode"`
		Bright  int `json:"bright"`
		Channel int `json:"channel"`
	} `json:"WhiteLed"`
}

func (c *Client) GetSpotlight(ctx context.Context) (*Spotlight, error) {
	var w whiteLedWrap
	if err := c.call(ctx, "GetWhiteLed", 1, map[string]any{"channel": 0}, &w); err != nil {
		return nil, err
	}
	return &Spotlight{State: w.WhiteLed.State, Mode: w.WhiteLed.Mode, Bright: w.WhiteLed.Bright}, nil
}

func (c *Client) SetSpotlight(ctx context.Context, s Spotlight) error {
	if s.Bright < 0 {
		s.Bright = 0
	}
	if s.Bright > 100 {
		s.Bright = 100
	}
	return c.call(ctx, "SetWhiteLed", 0, map[string]any{
		"WhiteLed": map[string]any{
			"channel": 0, "state": s.State, "mode": s.Mode, "bright": s.Bright,
		},
	}, nil)
}

// IrLights is the infrared illuminator: "Auto" or "Off".
type IrLights struct {
	State string `json:"state"`
}

func (c *Client) GetIR(ctx context.Context) (*IrLights, error) {
	var w struct {
		IrLights IrLights `json:"IrLights"`
	}
	if err := c.call(ctx, "GetIrLights", 1, map[string]any{"channel": 0}, &w); err != nil {
		return nil, err
	}
	return &w.IrLights, nil
}

func (c *Client) SetIR(ctx context.Context, state string) error {
	if state != "Auto" && state != "Off" {
		return fmt.Errorf("IR state must be Auto or Off")
	}
	return c.call(ctx, "SetIrLights", 0, map[string]any{
		"IrLights": map[string]any{"channel": 0, "state": state},
	}, nil)
}
