package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/ext"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Extension-defined settings (the manifest `settings` schema). The manager renders
// the schema as a form on the extension's row; values persist in the extension's
// own ext_kv namespace, so both SDK transports read them back exactly like storage.
// Scalars store a string; a `group` (a repeatable list of rows) stores a JSON array
// of {subkey: value} objects, so real configuration like a camera→entity mapping is
// expressible.
//
// Secret fields are write-only: their plaintext is never returned to a client — GET
// reports only whether a secret is set, and a secret is reachable in the clear only
// by the extension's own backend (server-side, via its token). Reads are `authed`
// (so a viewer's panel can pick up non-secret config); writes are admin-only, are
// validated against the declared types, and restart the backend so it re-reads them.

// handleExtSettingsGet returns the settings schema, the current NON-secret values
// (scalars as strings, groups as arrays of row objects), and which secrets are set.
func (s *Server) handleExtSettingsGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ex, err := s.st.GetExtension(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	m, err := ext.Parse([]byte(ex.ManifestJSON))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "manifest unreadable")
		return
	}
	values := map[string]any{}
	secretSet := map[string]bool{}
	for _, f := range m.Settings {
		v, found, gerr := s.st.ExtKVGet(id, f.Key)
		if gerr != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		switch f.Type {
		case "secret":
			secretSet[f.Key] = found && v != "" // presence only — never the value
		case "group":
			rows := []map[string]string{}
			if found && v != "" {
				_ = json.Unmarshal([]byte(v), &rows)
			}
			values[f.Key] = rows
		default:
			if found {
				values[f.Key] = v
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"schema":    m.Settings, // field metadata incl. group fields + defaults
		"values":    values,     // stored non-secret values (scalars: string, group: []row)
		"secretSet": secretSet,
	})
}

// handleExtSettingsPut validates a settings update against the declared schema and
// persists it, then restarts the backend (if any) so it re-reads the new values.
// Secrets are replace-if-non-empty: an empty/absent secret leaves the stored one
// untouched (the UI shows a masked "set" placeholder, never the value).
func (s *Server) handleExtSettingsPut(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ex, err := s.st.GetExtension(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	m, err := ext.Parse([]byte(ex.ManifestJSON))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "manifest unreadable")
		return
	}
	if len(m.Settings) == 0 {
		writeErrorCode(w, http.StatusBadRequest, "validation", "this extension has no settings")
		return
	}
	var req struct {
		Values map[string]json.RawMessage `json:"values"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	known := map[string]bool{}
	for _, f := range m.Settings {
		known[f.Key] = true
	}
	for k := range req.Values {
		if !known[k] {
			writeErrorCode(w, http.StatusBadRequest, "validation", "unknown setting "+strconv.Quote(k))
			return
		}
	}
	camAllowed := camAllowedSet(ex) // nil = "all" grants → accept any camera id

	// Validate everything before writing anything (so a bad field can't leave a
	// half-applied update).
	type pending struct{ key, value string }
	var writes []pending
	bad := func(msg string) { writeErrorCode(w, http.StatusBadRequest, "validation", msg) }

	for _, f := range m.Settings {
		raw, provided := req.Values[f.Key]
		switch f.Type {
		case "secret":
			val, nonEmpty, verr := coerceSetting(f, raw, provided, camAllowed)
			if verr != nil {
				bad(verr.Error())
				return
			}
			if !nonEmpty { // empty/absent secret → keep the existing one
				if f.Required {
					if _, found, _ := s.st.ExtKVGet(id, f.Key); !found {
						bad(f.Label + " is required")
						return
					}
				}
				continue
			}
			writes = append(writes, pending{f.Key, val})
		case "group":
			rows, verr := coerceGroup(f, raw, provided, camAllowed)
			if verr != nil {
				bad(verr.Error())
				return
			}
			if f.Required && len(rows) == 0 {
				bad(f.Label + " needs at least one entry")
				return
			}
			data, _ := json.Marshal(rows)
			writes = append(writes, pending{f.Key, string(data)})
		default:
			val, _, verr := coerceSetting(f, raw, provided, camAllowed)
			if verr != nil {
				bad(verr.Error())
				return
			}
			if f.Required && strings.TrimSpace(val) == "" {
				bad(f.Label + " is required")
				return
			}
			writes = append(writes, pending{f.Key, val})
		}
	}

	for _, p := range writes {
		if err := s.st.ExtKVSet(id, p.key, p.value); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save settings")
			return
		}
	}

	// Restart the backend so it re-reads settings (and re-resolves any url-setting
	// network host). A UI-only extension has nothing to restart.
	if ex.Enabled && s.extSup != nil {
		s.extSup.Stop(ex.ID)
		s.spawnIfBackend(ex)
	}
	s.handleExtSettingsGet(w, r) // echo the redacted current state
}

// camAllowedSet returns the set of camera ids a `camera` setting value may take, or
// nil when the extension is granted all cameras (any id is acceptable then).
func camAllowedSet(ex *store.Extension) map[string]bool {
	if len(ex.CameraGrants) == 0 {
		return nil
	}
	set := make(map[string]bool, len(ex.CameraGrants))
	for _, c := range ex.CameraGrants {
		set[c] = true
	}
	return set
}

// seedSettingDefaults writes the declared default of each scalar NON-secret setting
// at install, so its backend and panels see the intended value immediately. Secrets
// and groups are skipped (secrets must be entered; groups start empty). Never
// overwrites an existing value.
func (s *Server) seedSettingDefaults(id string, m *ext.Manifest) {
	for _, f := range m.Settings {
		if f.Type == "secret" || f.Type == "group" || len(f.Default) == 0 {
			continue
		}
		if _, found, _ := s.st.ExtKVGet(id, f.Key); found {
			continue
		}
		if val, ok, _ := coerceSetting(f, f.Default, true, nil); ok {
			_ = s.st.ExtKVSet(id, f.Key, val)
		}
	}
}

// coerceGroup validates + normalizes a repeatable group's rows into []{subkey:value}.
func coerceGroup(f ext.SettingField, raw json.RawMessage, provided bool, camAllowed map[string]bool) ([]map[string]string, error) {
	out := []map[string]string{}
	if !provided || len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("%s must be a list of entries", f.Label)
	}
	for i, row := range rows {
		obj := map[string]string{}
		for _, sub := range f.Fields {
			sraw, sprov := row[sub.Key]
			sv, _, err := coerceSetting(sub, sraw, sprov, camAllowed)
			if err != nil {
				return nil, fmt.Errorf("%s entry %d: %w", f.Label, i+1, err)
			}
			if sub.Required && strings.TrimSpace(sv) == "" {
				return nil, fmt.Errorf("%s entry %d: %s is required", f.Label, i+1, sub.Label)
			}
			obj[sub.Key] = sv
		}
		out = append(out, obj)
	}
	return out, nil
}

// coerceSetting converts one incoming JSON scalar to its stored string form, per the
// field's declared type. It returns (value, nonEmpty, error). An absent value is
// treated as empty.
func coerceSetting(f ext.SettingField, raw json.RawMessage, provided bool, camAllowed map[string]bool) (string, bool, error) {
	if !provided || len(raw) == 0 || string(raw) == "null" {
		return "", false, nil
	}
	switch f.Type {
	case "text", "secret":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", false, fmt.Errorf("%s must be text", f.Label)
		}
		return s, strings.TrimSpace(s) != "", nil
	case "url":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", false, fmt.Errorf("%s must be a URL", f.Label)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return "", false, nil
		}
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", false, fmt.Errorf("%s must be an http(s) URL, e.g. https://192.168.1.10:8123", f.Label)
		}
		return s, true, nil
	case "camera":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", false, fmt.Errorf("%s must be a camera", f.Label)
		}
		s = strings.TrimSpace(s)
		if s != "" && camAllowed != nil && !camAllowed[s] {
			return "", false, fmt.Errorf("%s: camera %q is not one this extension may use", f.Label, s)
		}
		return s, s != "", nil
	case "select":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", false, fmt.Errorf("%s must be one of its options", f.Label)
		}
		if s != "" {
			ok := false
			for _, o := range f.Options {
				if o.Value == s {
					ok = true
					break
				}
			}
			if !ok {
				return "", false, fmt.Errorf("%s: %q is not one of the allowed options", f.Label, s)
			}
		}
		return s, s != "", nil
	case "number":
		var n json.Number
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		if err := dec.Decode(&n); err == nil {
			return n.String(), true, nil
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if strings.TrimSpace(s) == "" {
				return "", false, nil
			}
			if _, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
				return strings.TrimSpace(s), true, nil
			}
		}
		return "", false, fmt.Errorf("%s must be a number", f.Label)
	case "boolean":
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return "", false, fmt.Errorf("%s must be true or false", f.Label)
		}
		if b {
			return "true", true, nil
		}
		return "false", true, nil
	}
	return "", false, fmt.Errorf("%s has an unsupported type", f.Label)
}
