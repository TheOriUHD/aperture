package store

import (
	"crypto/rand"
	"encoding/hex"
)

// Role enumerates access levels.
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

// Recording modes for a camera.
const (
	ModeNever      = "never"
	ModeContinuous = "continuous"
	ModeMotion     = "motion"
	ModeScheduled  = "scheduled"
)

// User is an account that can sign in.
type User struct {
	ID                 string  `json:"id"`
	Email              string  `json:"email"`
	Name               string  `json:"name"`
	PasswordHash       string  `json:"-"`
	Role               string  `json:"role"`
	OIDCSub            *string `json:"-"`
	Disabled           bool    `json:"disabled"`
	CreatedAt          int64   `json:"createdAt"`
	MustChangePassword bool    `json:"-"`
	TOTPSecret         string  `json:"-"`
	TOTPEnabled        bool    `json:"-"`
	TOTPRecovery       string  `json:"-"`
}

// Camera is a configured RTSP source.
type Camera struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	RTSPMainURL       string  `json:"rtspMainUrl"`
	RTSPSubURL        string  `json:"rtspSubUrl"`
	Enabled           bool    `json:"enabled"`
	RecordMode        string  `json:"recordMode"`
	RetentionDays     int     `json:"retentionDays"`
	StorageQuotaBytes int64   `json:"storageQuotaBytes"`
	MotionThreshold   float64 `json:"motionThreshold"`
	MotionZonesJSON   string  `json:"motionZonesJson"`
	FPS               int     `json:"fps"`
	SortOrder         int     `json:"sortOrder"`
	PrerollMs         int     `json:"prerollMs"`
	PostrollMs        int     `json:"postrollMs"`
	CooldownMs        int     `json:"cooldownMs"`
	MinDurationMs     int     `json:"minDurationMs"`
	CreatedAt         int64   `json:"createdAt"`
	UpdatedAt         int64   `json:"updatedAt"`
}

// NewID returns a random 128-bit hex identifier.
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("store: cannot read random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}
