// Package eufy manages the Eufy bridge: the account credentials Aperture holds for
// it, the bridge service's lifecycle, and the WebSocket session used to report
// status and answer sign-in challenges.
//
// Why this lives in Aperture rather than in the extension that uses it: Eufy
// publishes no local device API, so control goes through eufy-security-ws, which
// signs in to a Eufy cloud account. That server reads its credentials ONLY at
// startup — its driver command set has set_verify_code and set_captcha but nothing
// for username/password. An extension runs sandboxed under an unprivileged
// per-extension uid with write access to its own directory alone, so it cannot
// write that config or restart that service, and it should not be able to. Aperture
// already runs privileged, already stores camera credentials encrypted, and already
// owns the Settings surface, so the privileged half belongs here and the extension
// is left to do what it is good at: drawing controls.
package eufy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// schemaVersion is the eufy-security-ws API schema we ask for. Pan/tilt needs >= 3
// and device.calibrate >= 10; the server replies with its own maximum when it
// predates ours, and we adopt that.
const schemaVersion = 13

// Config is where the bridge lives and how it is managed.
type Config struct {
	// URL of the bridge's WebSocket, e.g. ws://127.0.0.1:3000.
	URL string
	// ConfigPath is the bridge's own JSON config, which Aperture rewrites when the
	// account changes.
	ConfigPath string
	// Unit is the systemd unit to restart after a credential change. Empty disables
	// service management (useful in tests and on hosts where the bridge is external).
	Unit string
}

// Device is one Eufy device as the bridge reports it.
type Device struct {
	SerialNumber string   `json:"serialNumber"`
	Name         string   `json:"name"`
	Model        string   `json:"model,omitempty"`
	Commands     []string `json:"commands,omitempty"`
	// PanTilt is the capability that decides whether a jog wheel is offered. Taken
	// from what the device reports it can do, never inferred from the model name.
	PanTilt bool `json:"panTilt"`
}

// Status is everything the Settings UI needs to render the bridge's state.
type Status struct {
	// Configured is whether an account has been saved at all.
	Configured bool `json:"configured"`
	// BridgeRunning is whether the bridge process is reachable on its socket.
	BridgeRunning bool `json:"bridgeRunning"`
	// Connected is whether the bridge is signed in to Eufy.
	Connected bool `json:"connected"`
	// NeedsVerifyCode / NeedsCaptcha are the two sign-in challenges. They are the
	// reason this needs a UI at all: both arrive asynchronously during sign-in and
	// there is nowhere else in the product to answer them.
	NeedsVerifyCode bool   `json:"needsVerifyCode"`
	NeedsCaptcha    bool   `json:"needsCaptcha"`
	CaptchaID       string `json:"captchaId,omitempty"`
	// CaptchaImage is a data: URI the UI can render directly.
	CaptchaImage string   `json:"captchaImage,omitempty"`
	Username     string   `json:"username,omitempty"`
	LastError    string   `json:"lastError,omitempty"`
	Devices      []Device `json:"devices"`
}

// Client keeps one long-lived session with the bridge.
//
// Long-lived rather than a connection per request because both sign-in challenges
// are pushed events: a captcha or a 2FA prompt arrives while the bridge is trying
// to authenticate, and a client that only connected when asked would miss it and
// leave the user staring at "connecting" forever.
type Client struct {
	cfg Config
	log *slog.Logger

	mu        sync.Mutex
	conn      *websocket.Conn
	status    Status
	pending   map[string]chan wsResult
	nextID    int
	stopped   bool
	lastTried time.Time
}

type wsResult struct {
	success bool
	result  json.RawMessage
	errCode string
}

func New(cfg Config, log *slog.Logger) *Client {
	return &Client{cfg: cfg, log: log, pending: map[string]chan wsResult{}}
}

// Start runs the connect/reconnect loop until ctx is done.
func (c *Client) Start(ctx context.Context) {
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			c.session(ctx)
			// Steady, unhurried retry: the bridge may be stopped for long stretches
			// (no account configured yet), and hammering a loopback socket that is
			// deliberately down is pure noise in the journal.
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
		}
	}()
}

func (c *Client) session(ctx context.Context) {
	u := strings.TrimSpace(c.cfg.URL)
	if u == "" {
		return
	}
	// Accept an http(s) URL and speak ws over it — that is what operators type, and
	// it is what the settings field validates as.
	if strings.HasPrefix(u, "http") {
		u = "ws" + strings.TrimPrefix(u, "http")
	}
	if _, err := url.Parse(u); err != nil {
		return
	}

	dialCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, _, err := websocket.DefaultDialer.DialContext(dialCtx, u, nil)
	if err != nil {
		c.setBridgeDown(err)
		return
	}
	defer conn.Close()

	c.mu.Lock()
	c.conn = conn
	c.status.BridgeRunning = true
	c.status.LastError = ""
	c.mu.Unlock()

	// The reader starts BEFORE the handshake, not after. send() blocks waiting for a
	// result message, and results only arrive through this loop — doing the handshake
	// first deadlocks every time, which reads as "timed out waiting for
	// set_api_schema" and looks like the bridge being unreachable rather than a bug
	// on this side.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var raw json.RawMessage
			if err := conn.ReadJSON(&raw); err != nil {
				c.setBridgeDown(err)
				return
			}
			c.handle(raw)
		}
	}()

	if _, err := c.send(ctx, "set_api_schema", map[string]any{"schemaVersion": schemaVersion}); err != nil {
		c.setBridgeDown(err)
		conn.Close()
		<-done
		return
	}
	if res, err := c.send(ctx, "start_listening", nil); err == nil {
		c.ingestState(res)
	}

	select {
	case <-done:
	case <-ctx.Done():
		conn.Close()
		<-done
	}
}

func (c *Client) setBridgeDown(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn = nil
	c.status.BridgeRunning = false
	c.status.Connected = false
	if err != nil {
		c.status.LastError = err.Error()
	}
}

func (c *Client) handle(raw json.RawMessage) {
	var env struct {
		Type      string          `json:"type"`
		MessageID string          `json:"messageId"`
		Success   bool            `json:"success"`
		Result    json.RawMessage `json:"result"`
		ErrorCode string          `json:"errorCode"`
		Event     struct {
			Source       string `json:"source"`
			Event        string `json:"event"`
			SerialNumber string `json:"serialNumber"`
			CaptchaID    string `json:"captchaId"`
			Captcha      string `json:"captcha"`
		} `json:"event"`
		MaxSchemaVersion int `json:"maxSchemaVersion"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return
	}

	switch env.Type {
	case "result":
		c.mu.Lock()
		ch, ok := c.pending[env.MessageID]
		delete(c.pending, env.MessageID)
		c.mu.Unlock()
		if ok {
			ch <- wsResult{success: env.Success, result: env.Result, errCode: env.ErrorCode}
		}
	case "event":
		c.mu.Lock()
		switch env.Event.Event {
		case "captcha request":
			c.status.NeedsCaptcha = true
			c.status.CaptchaID = env.Event.CaptchaID
			c.status.CaptchaImage = env.Event.Captcha
		case "verify code":
			c.status.NeedsVerifyCode = true
		case "connected":
			c.status.Connected = true
			c.status.NeedsCaptcha = false
			c.status.NeedsVerifyCode = false
			c.status.LastError = ""
		case "disconnected":
			c.status.Connected = false
		}
		c.mu.Unlock()
	}
}

func (c *Client) ingestState(res json.RawMessage) {
	// state.devices is a list of SERIAL NUMBER STRINGS, not device objects — the
	// details have to be fetched per serial. Decoding it as objects yields a list of
	// empty devices, which looks exactly like "signed in but no cameras found".
	var st struct {
		State struct {
			Driver struct {
				Connected bool `json:"connected"`
			} `json:"driver"`
			Devices []string `json:"devices"`
		} `json:"state"`
	}
	if err := json.Unmarshal(res, &st); err != nil {
		return
	}

	c.mu.Lock()
	c.status.Connected = st.State.Driver.Connected
	c.mu.Unlock()

	devices := make([]Device, 0, len(st.State.Devices))
	for _, serial := range st.State.Devices {
		d := Device{SerialNumber: serial, Name: serial}

		if raw, err := c.send(context.Background(), "device.get_properties",
			map[string]any{"serialNumber": serial}); err == nil {
			var props struct {
				Properties struct {
					Name  string `json:"name"`
					Model string `json:"model"`
				} `json:"properties"`
			}
			if json.Unmarshal(raw, &props) == nil {
				if props.Properties.Name != "" {
					d.Name = props.Properties.Name
				}
				d.Model = props.Properties.Model
			}
		}

		// Capabilities come from what the device reports, never from its model name.
		// Note the command names here are BARE ("pan_and_tilt"), not the "device."
		// prefixed form used to invoke them — matching on the prefixed name silently
		// reports every device as incapable.
		if raw, err := c.send(context.Background(), "device.get_commands",
			map[string]any{"serialNumber": serial}); err == nil {
			var got struct {
				Commands []string `json:"commands"`
			}
			if json.Unmarshal(raw, &got) == nil {
				d.Commands = got.Commands
				for _, cmd := range got.Commands {
					if cmd == "pan_and_tilt" || cmd == "device.pan_and_tilt" {
						d.PanTilt = true
					}
				}
			}
		}
		devices = append(devices, d)
	}

	c.mu.Lock()
	c.status.Devices = devices
	c.mu.Unlock()
}

func (c *Client) send(ctx context.Context, command string, extra map[string]any) (json.RawMessage, error) {
	c.mu.Lock()
	conn := c.conn
	c.nextID++
	id := fmt.Sprintf("ap-%d", c.nextID)
	ch := make(chan wsResult, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	if conn == nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("bridge is not connected")
	}

	msg := map[string]any{"messageId": id, "command": command}
	for k, v := range extra {
		msg[k] = v
	}
	if err := conn.WriteJSON(msg); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(15 * time.Second):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("timed out waiting for %s", command)
	case r := <-ch:
		if !r.success {
			if r.errCode == "" {
				r.errCode = command + " failed"
			}
			return nil, fmt.Errorf("%s", r.errCode)
		}
		return r.result, nil
	}
}

// Status returns a snapshot for the UI.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.status
	s.Devices = append([]Device(nil), c.status.Devices...)
	return s
}

// SetVerifyCode answers the emailed two-factor challenge.
func (c *Client) SetVerifyCode(ctx context.Context, code string) error {
	_, err := c.send(ctx, "driver.set_verify_code", map[string]any{"verifyCode": code})
	if err == nil {
		c.mu.Lock()
		c.status.NeedsVerifyCode = false
		c.mu.Unlock()
	}
	return err
}

// SetCaptcha answers a captcha challenge.
func (c *Client) SetCaptcha(ctx context.Context, id, code string) error {
	_, err := c.send(ctx, "driver.set_captcha", map[string]any{"captchaId": id, "captcha": code})
	if err == nil {
		c.mu.Lock()
		c.status.NeedsCaptcha = false
		c.status.CaptchaID = ""
		c.status.CaptchaImage = ""
		c.mu.Unlock()
	}
	return err
}

// SetMarkConfigured records whether an account has been saved, for the UI's benefit.
func (c *Client) SetMarkConfigured(configured bool, username string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.Configured = configured
	c.status.Username = username
}

// ApplyCredentials writes the account into the bridge's own config and restarts it.
//
// The bridge reads credentials only at startup, so a restart is not incidental — it
// is the only way a new account takes effect. Other keys in that file (persistent
// directory, polling intervals, trusted device name) are preserved: an operator may
// have tuned them, and a credential change should not silently revert their work.
func ApplyCredentials(cfg Config, username, password, country string) error {
	if cfg.ConfigPath == "" {
		return fmt.Errorf("no bridge config path configured")
	}
	existing := map[string]any{}
	if b, err := os.ReadFile(cfg.ConfigPath); err == nil {
		_ = json.Unmarshal(b, &existing)
	}
	existing["username"] = username
	existing["password"] = password
	if country != "" {
		existing["country"] = country
	}
	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}

	// Write-then-rename so the bridge can never observe a half-written config, and
	// 0640 because this file holds an account password.
	dir := filepath.Dir(cfg.ConfigPath)
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(append(out, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o640); err != nil {
		return err
	}
	// Keep whatever ownership the existing config had, so the bridge's own user can
	// still read it after the swap.
	if fi, err := os.Stat(cfg.ConfigPath); err == nil {
		if st, ok := fileOwner(fi); ok {
			_ = os.Chown(tmpName, st.uid, st.gid)
		}
	}
	if err := os.Rename(tmpName, cfg.ConfigPath); err != nil {
		return err
	}

	if cfg.Unit == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out2, err := exec.CommandContext(ctx, "systemctl", "restart", cfg.Unit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("restarting %s: %v: %s", cfg.Unit, err, strings.TrimSpace(string(out2)))
	}
	return nil
}
