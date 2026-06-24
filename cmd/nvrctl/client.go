package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client talks to the nvrd HTTP API. It authenticates either with a session
// cookie (interactive TUI login) or the local CLI bearer token (scripting).
type Client struct {
	baseURL string
	token   string // CLI bearer token
	session string // aperture_session cookie value
	http    *http.Client
}

func newClient(url, token string) *Client {
	return &Client{baseURL: strings.TrimRight(url, "/"), token: token, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) BaseURL() string     { return c.baseURL }
func (c *Client) Session() string     { return c.session }
func (c *Client) SetSession(s string) { c.session = s }
func (c *Client) HasSession() bool    { return c.session != "" }

// resolveToken returns the explicit token, or reads cli.token from likely paths.
func resolveToken(explicit string) string {
	if explicit != "" {
		return explicit
	}
	var cands []string
	if d := os.Getenv("APERTURE_DATA_DIR"); d != "" {
		cands = append(cands, filepath.Join(d, "cli.token"))
	}
	cands = append(cands, "/data/cli.token", "./data/cli.token", "./deploy/data/cli.token")
	for _, p := range cands {
		if b, err := os.ReadFile(p); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

// APIError carries an HTTP status and a human message.
type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string { return e.Msg }

func errFromResp(resp *http.Response) error {
	b, _ := io.ReadAll(resp.Body)
	msg := strings.TrimSpace(string(b))
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		msg = e.Error
	}
	if msg == "" {
		msg = resp.Status
	}
	return &APIError{Status: resp.StatusCode, Msg: msg}
}

func (c *Client) do(method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.session != "" {
		req.AddCookie(&http.Cookie{Name: "aperture_session", Value: c.session})
	} else if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errFromResp(resp)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (c *Client) get(path string, out any) error { return c.do(http.MethodGet, path, nil, out) }

// --- auth ----------------------------------------------------------------

// Login exchanges credentials for a session cookie, which is retained on the
// client and returned for persistence.
func (c *Client) Login(email, password string) (*userInfo, error) {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req, _ := http.NewRequest(http.MethodPost, c.baseURL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errFromResp(resp)
	}
	for _, ck := range resp.Cookies() {
		if ck.Name == "aperture_session" {
			c.session = ck.Value
		}
	}
	var u userInfo
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *Client) Me() (*userInfo, error) {
	var u userInfo
	if err := c.get("/api/auth/me", &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *Client) Logout() {
	_ = c.do(http.MethodPost, "/api/auth/logout", nil, nil)
	c.session = ""
}

// --- resources -----------------------------------------------------------

func (c *Client) Cameras() ([]cameraInfo, error) {
	var x []cameraInfo
	err := c.get("/api/cameras", &x)
	return x, err
}

func (c *Client) SaveCamera(id string, body any) error {
	if id == "" {
		return c.do(http.MethodPost, "/api/cameras", body, nil)
	}
	return c.do(http.MethodPut, "/api/cameras/"+id, body, nil)
}

func (c *Client) DeleteCamera(id string) error {
	return c.do(http.MethodDelete, "/api/cameras/"+id, nil, nil)
}

func (c *Client) Users() ([]userInfo, error) {
	var x []userInfo
	err := c.get("/api/users", &x)
	return x, err
}

func (c *Client) SaveUser(id string, body any) error {
	if id == "" {
		return c.do(http.MethodPost, "/api/users", body, nil)
	}
	return c.do(http.MethodPut, "/api/users/"+id, body, nil)
}

func (c *Client) DeleteUser(id string) error {
	return c.do(http.MethodDelete, "/api/users/"+id, nil, nil)
}

func (c *Client) Storage() (storageInfo, error) {
	var s storageInfo
	err := c.get("/api/system/storage", &s)
	return s, err
}

func (c *Client) Events(limit int) ([]eventInfo, error) {
	var x []eventInfo
	now := time.Now().UnixMilli()
	err := c.get(fmt.Sprintf("/api/events?from=%d&to=%d&limit=%d", now-7*24*3600*1000, now, limit), &x)
	return x, err
}

func (c *Client) SiteName() (string, error) {
	var x struct {
		SiteName string `json:"siteName"`
	}
	err := c.get("/api/system/settings", &x)
	return x.SiteName, err
}

func (c *Client) SetSiteName(name string) error {
	return c.do(http.MethodPut, "/api/system/settings", map[string]string{"siteName": name}, nil)
}

func (c *Client) HA() (haInfo, error) {
	var h haInfo
	err := c.get("/api/system/ha", &h)
	return h, err
}

func (c *Client) SetHA(body any) error { return c.do(http.MethodPut, "/api/system/ha", body, nil) }

func (c *Client) OIDC() (oidcInfo, error) {
	var o oidcInfo
	err := c.get("/api/system/oidc", &o)
	return o, err
}

func (c *Client) SetOIDC(body any) error { return c.do(http.MethodPut, "/api/system/oidc", body, nil) }

func (c *Client) Purge(cameraID string, from, to int64) (purgeResult, error) {
	var pr purgeResult
	body := map[string]any{"from": from, "to": to}
	if cameraID != "" {
		body["cameraId"] = cameraID
	}
	err := c.do(http.MethodPost, "/api/system/purge", body, &pr)
	return pr, err
}

// --- API response shapes -------------------------------------------------

type verInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type camStorage struct {
	CameraID string `json:"cameraId"`
	Name     string `json:"name"`
	Bytes    int64  `json:"bytes"`
	Count    int64  `json:"count"`
}

type storageInfo struct {
	RecordedBytes  int64        `json:"recordedBytes"`
	DiskTotalBytes int64        `json:"diskTotalBytes"`
	DiskFreeBytes  int64        `json:"diskFreeBytes"`
	Cameras        []camStorage `json:"cameras"`
}

type cameraInfo struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	RTSPMainURL     string  `json:"rtspMainUrl"`
	RTSPSubURL      string  `json:"rtspSubUrl"`
	Enabled         bool    `json:"enabled"`
	RecordMode      string  `json:"recordMode"`
	RetentionDays   int     `json:"retentionDays"`
	MotionThreshold float64 `json:"motionThreshold"`
	FPS             int     `json:"fps"`
}

type userInfo struct {
	ID        string   `json:"id"`
	Email     string   `json:"email"`
	Name      string   `json:"name"`
	Role      string   `json:"role"`
	Disabled  bool     `json:"disabled"`
	CameraIDs []string `json:"cameraIds"`
}

type eventInfo struct {
	ID       string  `json:"id"`
	CameraID string  `json:"cameraId"`
	Type     string  `json:"type"`
	StartTS  int64   `json:"startTs"`
	EndTS    int64   `json:"endTs"`
	Score    float64 `json:"score"`
}

type haInfo struct {
	Enabled     bool   `json:"enabled"`
	Broker      string `json:"broker"`
	Username    string `json:"username"`
	Prefix      string `json:"prefix"`
	HasPassword bool   `json:"hasPassword"`
	Connected   bool   `json:"connected"`
}

type oidcInfo struct {
	Enabled     bool   `json:"enabled"`
	Issuer      string `json:"issuer"`
	ClientID    string `json:"clientId"`
	Domain      string `json:"domain"`
	AutoCreate  bool   `json:"autoCreate"`
	Name        string `json:"name"`
	HasSecret   bool   `json:"hasSecret"`
	RedirectURL string `json:"redirectUrl"`
}

type purgeResult struct {
	DeletedSegments int   `json:"deletedSegments"`
	DeletedEvents   int   `json:"deletedEvents"`
	FreedBytes      int64 `json:"freedBytes"`
}

func startOfTodayMs() int64 {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location()).UnixMilli()
}

func fmtBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if v < 10 && i > 0 {
		return fmt.Sprintf("%.1f %s", v, units[i])
	}
	return fmt.Sprintf("%.0f %s", v, units[i])
}
