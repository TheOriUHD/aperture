package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/ext"
)

// Extension-defined settings (the manifest `settings` schema). The manager renders
// the schema as a form on the extension's row; values persist in the extension's
// own ext_kv namespace, so both SDK transports read them back exactly like storage.
//
// Secret fields are write-only: their plaintext is never returned to a client — GET
// reports only whether a secret is set, and a secret is reachable in the clear only
// by the extension's own backend (server-side, via its token). Reads are `authed`
// (so a viewer's panel can pick up non-secret config); writes are admin-only, are
// validated against the declared types, and restart the backend so it re-reads them.

// handleExtSettingsGet returns the settings schema, the current NON-secret values,
// and which secrets are set. It never returns a secret value.
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
	values := map[string]string{}
	secretSet := map[string]bool{}
	for _, f := range m.Settings {
		v, found, gerr := s.st.ExtKVGet(id, f.Key)
		if gerr != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if f.Type == "secret" {
			secretSet[f.Key] = found && v != "" // presence only — never the value
			continue
		}
		if found {
			values[f.Key] = v
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"schema":    m.Settings, // field metadata incl. defaults; frontend applies defaults for unset keys
		"values":    values,     // stored non-secret values only
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
	// Reject keys that are not in the schema, so a typo fails loudly.
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

	// Validate everything before writing anything (so a bad field can't leave a
	// half-applied update).
	type pending struct {
		key   string
		value string
		write bool
	}
	var writes []pending
	for _, f := range m.Settings {
		raw, provided := req.Values[f.Key]
		if f.Type == "secret" {
			val, nonEmpty, verr := coerceSetting(f, raw, provided)
			if verr != nil {
				writeErrorCode(w, http.StatusBadRequest, "validation", verr.Error())
				return
			}
			if !nonEmpty { // empty/absent secret → keep the existing one
				if f.Required {
					if _, found, _ := s.st.ExtKVGet(id, f.Key); !found {
						writeErrorCode(w, http.StatusBadRequest, "validation", f.Label+" is required")
						return
					}
				}
				continue
			}
			writes = append(writes, pending{f.Key, val, true})
			continue
		}
		val, _, verr := coerceSetting(f, raw, provided)
		if verr != nil {
			writeErrorCode(w, http.StatusBadRequest, "validation", verr.Error())
			return
		}
		if f.Required && strings.TrimSpace(val) == "" {
			writeErrorCode(w, http.StatusBadRequest, "validation", f.Label+" is required")
			return
		}
		writes = append(writes, pending{f.Key, val, true})
	}

	for _, p := range writes {
		if err := s.st.ExtKVSet(id, p.key, p.value); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save settings")
			return
		}
	}

	// Restart the backend so it re-reads settings. A UI-only extension has nothing to
	// restart; a disabled one starts fresh when re-enabled.
	if ex.Enabled && s.extSup != nil {
		s.extSup.Stop(ex.ID)
		s.spawnIfBackend(ex)
	}
	s.handleExtSettingsGet(w, r) // echo the redacted current state
}

// seedSettingDefaults writes the declared default of each NON-secret setting into
// the extension's namespace at install, so its backend and panels see the intended
// value immediately — before the admin has opened the settings form. Secrets have
// no default (they must be entered). Never overwrites an existing value.
func (s *Server) seedSettingDefaults(id string, m *ext.Manifest) {
	for _, f := range m.Settings {
		if f.Type == "secret" || len(f.Default) == 0 {
			continue
		}
		if _, found, _ := s.st.ExtKVGet(id, f.Key); found {
			continue
		}
		if val, ok, _ := coerceSetting(f, f.Default, true); ok {
			_ = s.st.ExtKVSet(id, f.Key, val)
		}
	}
}

// coerceSetting converts one incoming JSON value to its stored string form, per the
// field's declared type. It returns (value, nonEmpty, error). An absent value is
// treated as empty. select values are checked against the declared options.
func coerceSetting(f ext.SettingField, raw json.RawMessage, provided bool) (string, bool, error) {
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
		// Accept a JSON number or a numeric string; store the canonical form.
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
		return "false", true, nil // a boolean is always meaningfully set
	}
	return "", false, fmt.Errorf("%s has an unsupported type", f.Label)
}
