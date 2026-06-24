package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// sessionTTL is how long a TUI login is remembered on disk before requiring a
// fresh sign-in (matches the server session lifetime).
const sessionTTL = 7 * 24 * time.Hour

type savedSession struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	Email   string `json:"email"`
	Expires int64  `json:"expires"` // unix seconds
}

func sessionPath() string {
	if p := os.Getenv("APERTURE_SESSION_FILE"); p != "" {
		return p
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, ".local", "state")
		} else {
			base = os.TempDir()
		}
	}
	return filepath.Join(base, "aperture", "nvrctl-session.json")
}

func loadSession() *savedSession {
	b, err := os.ReadFile(sessionPath())
	if err != nil {
		return nil
	}
	var s savedSession
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	if s.Token == "" || s.Expires < time.Now().Unix() {
		return nil
	}
	return &s
}

func saveSession(url, token, email string) {
	p := sessionPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	b, _ := json.MarshalIndent(savedSession{
		URL:     url,
		Token:   token,
		Email:   email,
		Expires: time.Now().Add(sessionTTL).Unix(),
	}, "", "  ")
	_ = os.WriteFile(p, b, 0o600)
}

func clearSession() { _ = os.Remove(sessionPath()) }
