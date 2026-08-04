// Package config loads Aperture's runtime configuration from environment
// variables, applies sane defaults, and manages persisted secrets.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is the fully-resolved runtime configuration.
type Config struct {
	HTTPAddr      string // e.g. ":8080"
	DataDir       string // SQLite, secrets, runtime config
	RecordingsDir string // recorded segments root
	PublicURL     string // base URL used to build externally-shared stream URLs

	Go2RTCURL  string // go2rtc HTTP API base, e.g. http://go2rtc:1984
	Go2RTCRTSP string // go2rtc RTSP restream base, e.g. rtsp://go2rtc:8554

	MQTTURL string // tcp://host:1883, empty disables HA publishing

	// ONVIF Profile S provider (external VMS/NVR consumption). OFF by default.
	ONVIFEnabled bool   // enable the ONVIF SOAP server + WS-Discovery
	ONVIFAddr    string // ONVIF SOAP HTTP listen address, e.g. ":10101"

	// HomeKit bridge (Apple Home app). OFF by default.
	HomeKitEnabled bool

	WebDir string // serve UI from disk (dev) instead of the embedded bundle

	// Secrets (auto-generated + persisted if unset).
	JWTSecret     []byte
	StreamSecret  []byte
	CLIToken      []byte // local admin token for nvrctl (also written to cli.token)
	EncryptionKey []byte // 32-byte AES-256 key for at-rest storage-credential encryption
}

// CLITokenPath returns where the CLI token is written for nvrctl to read.
func (c *Config) CLITokenPath() string { return filepath.Join(c.DataDir, "cli.token") }

// DBPath returns the SQLite database path under the data directory.
func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "aperture.db") }

// Go2RTCConfigPath returns where nvrd may render go2rtc config if needed.
func (c *Config) Go2RTCConfigPath() string { return filepath.Join(c.DataDir, "go2rtc.yaml") }

// FrameCacheDir holds extracted hover-scrub preview JPEGs.
func (c *Config) FrameCacheDir() string { return filepath.Join(c.DataDir, "frame-cache") }

// SpoolDir is the local staging area ffmpeg writes to for cameras that record to
// a network location; a background uploader drains it to the backend. Keeping it
// under DataDir decouples recording from the destination — a network blip never
// interrupts ffmpeg.
func (c *Config) SpoolDir() string { return filepath.Join(c.DataDir, "spool") }

// RemoteCacheDir holds segments pulled back from a network location for tools
// that need a local file (scrub thumbnails, exports).
func (c *Config) RemoteCacheDir() string { return filepath.Join(c.DataDir, "rcache") }

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// Load resolves configuration from the environment, ensures required
// directories exist, and loads or generates persisted secrets.
func Load() (*Config, error) {
	c := &Config{
		HTTPAddr:       env("APERTURE_HTTP_ADDR", ":8080"),
		DataDir:        env("APERTURE_DATA_DIR", "./data"),
		RecordingsDir:  env("APERTURE_RECORDINGS_DIR", "./recordings"),
		PublicURL:      strings.TrimRight(env("APERTURE_PUBLIC_URL", "http://localhost:8080"), "/"),
		Go2RTCURL:      strings.TrimRight(env("APERTURE_GO2RTC_URL", "http://localhost:1984"), "/"),
		Go2RTCRTSP:     strings.TrimRight(env("APERTURE_GO2RTC_RTSP", "rtsp://localhost:8554"), "/"),
		MQTTURL:        env("APERTURE_MQTT_URL", ""),
		ONVIFEnabled:   env("APERTURE_ONVIF_ENABLED", "false") == "true",
		ONVIFAddr:      env("APERTURE_ONVIF_ADDR", ":10101"),
		HomeKitEnabled: env("APERTURE_HOMEKIT_ENABLED", "false") == "true",
		WebDir:         env("APERTURE_WEB_DIR", ""),
	}

	for _, dir := range []string{c.DataDir, c.RecordingsDir, c.FrameCacheDir(), c.SpoolDir(), c.RemoteCacheDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	if err := c.loadSecrets(); err != nil {
		return nil, err
	}
	return c, nil
}

type secretsFile struct {
	JWTSecret    string `json:"jwt_secret"`
	StreamSecret string `json:"stream_secret"`
	CLIToken     string `json:"cli_token"`
	EncKey       string `json:"enc_key"` // hex of a 32-byte AES-256 key
}

// loadSecrets resolves JWT and stream-signing secrets, preferring environment
// overrides, then a persisted secrets file, otherwise generating and saving
// fresh random secrets.
func (c *Config) loadSecrets() error {
	path := filepath.Join(c.DataDir, "secrets.json")
	sf := secretsFile{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &sf)
	}

	jwt := env("APERTURE_JWT_SECRET", sf.JWTSecret)
	stream := env("APERTURE_STREAM_SECRET", sf.StreamSecret)
	cli := sf.CLIToken
	encKey := sf.EncKey

	if jwt == "" {
		jwt = randHex(32)
	}
	if stream == "" {
		stream = randHex(32)
	}
	if cli == "" {
		cli = randHex(24)
	}
	// The at-rest encryption key must decode to exactly 32 bytes; (re)generate if
	// missing or malformed. Losing it only invalidates stored share credentials
	// (users re-enter them) — never footage or logins.
	if kb, err := hex.DecodeString(encKey); err != nil || len(kb) != 32 {
		encKey = randHex(32)
	}

	// Persist if anything was newly generated and not pinned via env.
	if sf.JWTSecret != jwt || sf.StreamSecret != stream || sf.CLIToken != cli || sf.EncKey != encKey {
		out, _ := json.MarshalIndent(secretsFile{JWTSecret: jwt, StreamSecret: stream, CLIToken: cli, EncKey: encKey}, "", "  ")
		if err := os.WriteFile(path, out, 0o600); err != nil {
			return fmt.Errorf("persist secrets: %w", err)
		}
	}

	c.JWTSecret = []byte(jwt)
	c.StreamSecret = []byte(stream)
	c.CLIToken = []byte(cli)
	c.EncryptionKey, _ = hex.DecodeString(encKey) // validated above → exactly 32 bytes

	// Mirror the CLI token to a plain file so nvrctl can read it locally.
	_ = os.WriteFile(c.CLITokenPath(), []byte(cli), 0o600)
	return nil
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("aperture: cannot read random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}
