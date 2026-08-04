// Package storage abstracts the destinations Aperture records footage to: the
// local disk today, plus network shares (SMB, SFTP). Segments carry a
// backend-relative path ("<cameraID>/<epoch>.ts") for network locations and an
// absolute path for local ones, so serving, deletion and accounting stay uniform.
package storage

import "io"

// Backend types.
const (
	TypeLocal = "local"
	TypeSMB   = "smb"
	TypeSFTP  = "sftp"
)

// Spec describes a destination well enough to build a Backend or run a Probe. It
// carries the DECRYPTED secret, so it is assembled at the API/registry layer and
// never persisted or logged.
type Spec struct {
	Type   string         // local | smb | sftp
	Config map[string]any // non-secret fields (path, host, port, share, user, subfolder, hostKey…)
	Secret string         // decrypted password or private key ("" = none)
}

// Stat is a capacity snapshot for a backend's filesystem/share.
type Stat struct {
	Total     int64
	Free      int64
	Supported bool // false when the backend can't report capacity (e.g. some SFTP servers)
}

// ReadSeekCloser is what Open returns for HTTP range serving.
type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

// Backend is a storage destination footage is written to, served from, and pruned
// on. rel paths are backend-relative and use forward slashes.
type Backend interface {
	// EnsureDir makes sure the directory holding rel exists.
	EnsureDir(rel string) error
	// Put atomically writes the local file at localPath to rel and returns the
	// number of bytes written. Implementations write to a temp name then rename.
	Put(localPath, rel string) (int64, error)
	// Open opens rel for reading (seekable) and returns its size.
	Open(rel string) (ReadSeekCloser, int64, error)
	// Remove deletes rel (a missing file is not an error).
	Remove(rel string) error
	// Stat reports capacity for the backend.
	Stat() (Stat, error)
	// Close releases any underlying connections.
	Close() error
}

// StepStatus values for a Probe checklist line.
const (
	StepPass = "pass"
	StepFail = "fail"
	StepWarn = "warn"
	StepSkip = "skip"
)

// Step is one line in a connection checklist (mirrors the wizard's UI).
type Step struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// ProbeResult is the outcome of testing a Spec without saving it.
type ProbeResult struct {
	OK             bool   `json:"ok"`
	Steps          []Step `json:"steps"`
	DiskTotalBytes int64  `json:"diskTotalBytes,omitempty"`
	DiskFreeBytes  int64  `json:"diskFreeBytes,omitempty"`
	HostKey        string `json:"hostKey,omitempty"` // SFTP: observed SSH host-key fingerprint, for pinning
}

// cfgStr reads a string field from a Spec's Config (trimmed of surrounding use by
// callers as needed).
func cfgStr(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	s, _ := cfg[key].(string)
	return s
}

// cfgInt reads an integer-ish field (JSON numbers decode to float64).
func cfgInt(cfg map[string]any, key string, def int) int {
	if cfg == nil {
		return def
	}
	switch v := cfg[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return def
}
