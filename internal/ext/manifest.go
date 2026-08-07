// Package ext implements the Aperture extension system: manifest parsing and
// permission mapping (T1.2), the Deno backend supervisor (T1.3), and supporting
// types. It is deliberately free of any dependency on internal/api so that
// internal/api can depend on it (the handlers and the broker live there). The
// scope strings this package emits are the same contract strings internal/api
// enforces; a cross-check test in package api pins that they stay in sync.
package ext

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Manifest is manifest.json at the root of an extension bundle. Parsing is strict
// (unknown fields rejected) so a typo in a permission key fails loudly at install
// rather than silently granting something other than the author intended.
type Manifest struct {
	Manifest      int          `json:"manifest"`
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Version       string       `json:"version"`
	Author        string       `json:"author"`
	Description   string       `json:"description"`
	MinAPIVersion string       `json:"minApiVersion"`
	Permissions   Permissions  `json:"permissions"`
	UI            *UISpec      `json:"ui,omitempty"`
	Backend       *BackendSpec `json:"backend,omitempty"`
	Update        *UpdateSpec  `json:"update,omitempty"`
	// Icon is an OPTIONAL bundle-relative path (under ui/, a .png) used as the
	// nav-rail icon when the user pins the extension (T2.1). It is served as an
	// <img> from /ext/{id}/* under the extension CSP and validated to be a small
	// PNG at install — never inlined into host DOM — so a hostile bundle cannot run
	// script in the user's session via the rail. Absent = a default host icon.
	Icon string `json:"icon,omitempty"`
}

// Permissions is the closed taxonomy of everything an extension may do. Anything
// not expressible here is not grantable — notably the session-only operations
// (tokens/users/backup/restore/restart/reindex/purge), which have no scope at all.
type Permissions struct {
	Data    map[string]string `json:"data,omitempty"`    // domain -> "read"|"write" (media also "live")
	Alarm   string            `json:"alarm,omitempty"`   // "arm"
	System  string            `json:"system,omitempty"`  // "read"
	Cameras []string          `json:"cameras,omitempty"` // camera-id grants (composed as owner ∩ grants)
	Network []string          `json:"network,omitempty"` // outbound URLs (backend only)
	Storage bool              `json:"storage,omitempty"` // its own namespaced ext_kv
}

type UISpec struct {
	Panels []Panel `json:"panels"`
}

// Panel places a UI extension at a named host slot. Slot is validated against the
// slots the web host actually exposes (T1.6).
type Panel struct {
	Slot  string `json:"slot"`
	Entry string `json:"entry"`
	Title string `json:"title"`
}

type BackendSpec struct {
	Entry       string `json:"entry"`
	DenoVersion string `json:"denoVersion,omitempty"`
}

type UpdateSpec struct {
	URL string `json:"url"`
}

// Derived is the authority + runtime inputs a valid manifest resolves to. The
// Deno flag string itself is assembled by the supervisor (T1.3), which knows the
// runtime paths; this stays pure.
type Derived struct {
	Scopes       []string // API scopes for the extension's token / broker claim
	CameraGrants []string // camera ids
	NetHosts     []string // host[:port] for Deno --allow-net (backend)
	ConsentLines []string // plain-language lines for the consent screen
	HasBackend      bool
	HasUI           bool
	HasNavPanel     bool   // declares a full-page nav-panel view → eligible to be pinned (T2.1)
	Icon            string // validated bundle-relative icon path (under ui/, .png), or ""
	RequestsCameras bool   // has a camera-touching scope → the consent screen offers camera narrowing
}

const ManifestVersion = 1

var (
	reID     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`) // reverse-DNS
	reSemver = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

	// knownSlots are the host mount points Pass 1 exposes (T1.6).
	knownSlots = map[string]bool{"camera-detail": true, "settings": true, "dashboard-card": true, "nav-panel": true}

	// dataDomainScopes maps a data domain + value to the exact scope strings in
	// internal/api. "write" implies "read". Absence of a value = not grantable.
	dataDomainScopes = map[string]map[string][]string{
		"cameras":    {"read": {"cameras:read"}, "write": {"cameras:read", "cameras:write"}},
		"events":     {"read": {"events:read"}, "write": {"events:read", "events:write"}},
		"coverage":   {"read": {"coverage:read"}},
		"segments":   {"read": {"segments:read"}},
		"media":      {"read": {"media:read"}, "write": {"media:read", "media:write"}, "live": {"media:live"}},
		"storage":    {"read": {"storage:read"}, "write": {"storage:read", "storage:write"}},
		"schedules":  {"read": {"schedules:read"}, "write": {"schedules:read", "schedules:write"}},
		"zones":      {"read": {"zones:read"}, "write": {"zones:read", "zones:write"}},
		"detection":  {"read": {"detection:read"}, "write": {"detection:read", "detection:write"}},
		"site":       {"read": {"site:read"}, "write": {"site:read", "site:write"}},
		"automation": {"read": {"automation:read"}, "write": {"automation:read", "automation:write"}},
	}
)

// AllMappableScopes returns every scope string the permission mapping can emit —
// including alarm:arm and system:read. A cross-check test in package api asserts
// each is a real, assignable scope, so this mapping cannot drift out of sync with
// the enforcement taxonomy.
func AllMappableScopes() []string {
	set := map[string]bool{"alarm:arm": true, "system:read": true}
	for _, vals := range dataDomainScopes {
		for _, scopes := range vals {
			for _, s := range scopes {
				set[s] = true
			}
		}
	}
	return sortedKeys(set)
}

// Parse strictly decodes a manifest and checks its schema version. It does not
// validate contents — call Validate for that.
func Parse(b []byte) (*Manifest, error) {
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("invalid manifest json: %w", err)
	}
	if m.Manifest != ManifestVersion {
		return nil, fmt.Errorf("unsupported manifest version %d (this server understands %d)", m.Manifest, ManifestVersion)
	}
	return &m, nil
}

// Validate checks structure and maps permissions, returning the resolved
// authority. A single call is the "is this installable?" gate: it rejects bad
// ids/versions, unsafe entry paths, incoherent UI/backend/network declarations,
// and un-grantable or misspelled permissions.
func (m *Manifest) Validate() (Derived, error) {
	var d Derived
	if !reID.MatchString(m.ID) {
		return d, fmt.Errorf("id %q must be a reverse-DNS identifier (e.g. com.example.thing)", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return d, fmt.Errorf("name is required")
	}
	if !reSemver.MatchString(m.Version) {
		return d, fmt.Errorf("version %q must be semver (e.g. 1.2.0)", m.Version)
	}
	if m.MinAPIVersion != "" && m.MinAPIVersion != "v1" {
		return d, fmt.Errorf("minApiVersion %q is newer than this server (v1)", m.MinAPIVersion)
	}

	d.HasUI = m.UI != nil && len(m.UI.Panels) > 0
	d.HasBackend = m.Backend != nil && m.Backend.Entry != ""
	if !d.HasUI && !d.HasBackend {
		return d, fmt.Errorf("an extension must declare at least one of ui.panels or backend.entry")
	}

	if m.UI != nil {
		for i, p := range m.UI.Panels {
			if !knownSlots[p.Slot] {
				return d, fmt.Errorf("ui.panels[%d]: unknown slot %q", i, p.Slot)
			}
			if err := safeEntry(p.Entry); err != nil {
				return d, fmt.Errorf("ui.panels[%d].entry: %w", i, err)
			}
			if p.Slot == "nav-panel" {
				d.HasNavPanel = true
			}
		}
	}

	// Optional nav-rail icon: a small PNG inside ui/ (so the existing enabled-only,
	// CSP'd asset route serves it) — the host renders it as an <img>, never inlined.
	// The path is validated here; the bytes (real PNG, bounded dimensions) are
	// validated at install where the file is available.
	if m.Icon != "" {
		if err := safeEntry(m.Icon); err != nil {
			return d, fmt.Errorf("icon: %w", err)
		}
		if !strings.HasPrefix(m.Icon, "ui/") || !strings.HasSuffix(strings.ToLower(m.Icon), ".png") {
			return d, fmt.Errorf("icon %q must be a .png inside ui/ (e.g. ui/icon.png)", m.Icon)
		}
		d.Icon = m.Icon
	}
	if d.HasBackend {
		if err := safeEntry(m.Backend.Entry); err != nil {
			return d, fmt.Errorf("backend.entry: %w", err)
		}
	}

	// Permissions → scopes.
	scopes := map[string]bool{}
	for domain, value := range m.Permissions.Data {
		vals, ok := dataDomainScopes[domain]
		if !ok {
			return d, fmt.Errorf("permissions.data: unknown domain %q", domain)
		}
		grant, ok := vals[value]
		if !ok {
			return d, fmt.Errorf("permissions.data.%s: %q is not grantable for this domain", domain, value)
		}
		for _, s := range grant {
			scopes[s] = true
		}
	}
	if m.Permissions.Alarm != "" {
		if m.Permissions.Alarm != "arm" {
			return d, fmt.Errorf("permissions.alarm: only %q is valid", "arm")
		}
		scopes["alarm:arm"] = true
	}
	if m.Permissions.System != "" {
		if m.Permissions.System != "read" {
			return d, fmt.Errorf("permissions.system: only %q is valid", "read")
		}
		scopes["system:read"] = true
	}
	d.Scopes = sortedKeys(scopes)
	d.RequestsCameras = touchesCameras(d.Scopes)

	// Camera grants. The manifest's list is a REQUEST — a ceiling the installing
	// user narrows at consent — not a fixed setting; empty means "all your cameras".
	for _, c := range m.Permissions.Cameras {
		if strings.TrimSpace(c) == "" {
			return d, fmt.Errorf("permissions.cameras: empty camera id")
		}
	}
	d.CameraGrants = append([]string(nil), m.Permissions.Cameras...)

	// Network (outbound) — backend only.
	if len(m.Permissions.Network) > 0 {
		if !d.HasBackend {
			return d, fmt.Errorf("permissions.network requires a backend (a UI-only extension cannot make outbound requests)")
		}
		for _, raw := range m.Permissions.Network {
			host, err := netHost(raw)
			if err != nil {
				return d, fmt.Errorf("permissions.network: %w", err)
			}
			d.NetHosts = append(d.NetHosts, host)
		}
	}

	d.ConsentLines = consentLines(m, d)
	return d, nil
}

// safeEntry rejects absolute paths and traversal so an entry can only reference a
// file inside the bundle.
func safeEntry(p string) error {
	if p == "" {
		return fmt.Errorf("is required")
	}
	if path.IsAbs(p) || strings.HasPrefix(p, "/") || strings.Contains(p, "..") || path.Clean(p) != p {
		return fmt.Errorf("%q must be a relative path inside the bundle (no leading / or ..)", p)
	}
	return nil
}

// netHost extracts the host[:port] Deno --allow-net wants from a declared URL.
func netHost(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("%q must be an absolute http(s) URL (e.g. https://homeassistant.local:8123)", raw)
	}
	return u.Host, nil
}

func consentLines(m *Manifest, d Derived) []string {
	var lines []string
	if len(d.Scopes) > 0 {
		verb := scopeSummary(d.Scopes)
		if len(d.CameraGrants) > 0 {
			lines = append(lines, fmt.Sprintf("%s — only for the cameras: %s", verb, strings.Join(d.CameraGrants, ", ")))
		} else if touchesCameras(d.Scopes) {
			lines = append(lines, verb+" — for all your cameras")
		} else {
			lines = append(lines, verb)
		}
	}
	for _, h := range d.NetHosts {
		lines = append(lines, fmt.Sprintf("Connect over the internet to %s — it can send your data there", h))
	}
	if m.Permissions.Storage {
		lines = append(lines, "Keep its own settings in a private folder")
	}
	lines = append(lines, "It cannot read your recordings files, your database, or your passwords.")
	return lines
}

func scopeSummary(scopes []string) string {
	domains := map[string]bool{}
	write := false
	for _, s := range scopes {
		parts := strings.SplitN(s, ":", 2)
		domains[parts[0]] = true
		if len(parts) == 2 && (parts[1] == "write" || parts[1] == "arm") {
			write = true
		}
	}
	names := sortedKeys(domains)
	verb := "Read"
	if write {
		verb = "Read and change"
	}
	return fmt.Sprintf("%s your %s", verb, strings.Join(names, ", "))
}

func touchesCameras(scopes []string) bool {
	for _, s := range scopes {
		switch strings.SplitN(s, ":", 2)[0] {
		case "cameras", "events", "coverage", "media", "segments":
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// reRemoteImport matches static import/export...from and dynamic import() targets
// that are remote (http/https) or otherwise escape the bundle (absolute/file:).
var reRemoteImport = regexp.MustCompile(`(?m)(?:\bfrom\s*|\bimport\s*\(\s*)['"]([^'"]+)['"]`)

// CheckNoRemoteImports statically scans backend source for module specifiers that
// Deno's --deny-import would block at runtime, so the failure is a clear
// install-time message instead of an opaque runtime permission error (R6). It
// returns the offending specifiers (empty = clean). Undecidable dynamic cases
// (computed specifiers) remain covered by --deny-import at runtime.
func CheckNoRemoteImports(src string) []string {
	var bad []string
	for _, mm := range reRemoteImport.FindAllStringSubmatch(src, -1) {
		spec := mm[1]
		switch {
		case strings.HasPrefix(spec, "http://"), strings.HasPrefix(spec, "https://"),
			strings.HasPrefix(spec, "file:"), path.IsAbs(spec):
			bad = append(bad, spec)
		}
	}
	return bad
}
