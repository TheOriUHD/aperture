package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client talks to the nvrd HTTP API using the local CLI bearer token.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func newClient(url, token string) *Client {
	return &Client{baseURL: strings.TrimRight(url, "/"), token: token, http: &http.Client{Timeout: 10 * time.Second}}
}

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

func (c *Client) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s: %s", path, resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// --- API response shapes (subset) ----------------------------------------

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
	ID          string `json:"id"`
	Name        string `json:"name"`
	RTSPMainURL string `json:"rtspMainUrl"`
	Enabled     bool   `json:"enabled"`
	RecordMode  string `json:"recordMode"`
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
