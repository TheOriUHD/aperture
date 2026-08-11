package api

import (
	"net/http"
	"strings"

	"github.com/TheOriUHD/aperture/internal/store"
)

// The Home Assistant Live Card integration, made usable without hand-assembling a
// scoped token. Two conveniences live here:
//
//   - A single, server-managed "live key" (settings key integrations.live_key). It is
//     minted on demand, revealable and re-rollable from the UI, and grants live view of
//     every camera — no scope/camera picking. It is NOT a normal api_token: it carries
//     exactly one capability (open a live WebSocket) and is validated by a dedicated
//     branch in authorizeStreamWS. Rerolling it invalidates the old value instantly.
//     Trade, stated plainly in the UI: anyone who can read a dashboard's card config can
//     read this key and thus watch any camera live. Reroll if that ever matters.
//
//   - Camera addressing by a human slug ("Zimmer PH" → "zimmer_ph") instead of the raw
//     128-bit id, resolved on the fly (no migration, always reflects the current name).
//
// The live-WS route accepts either form for {id}; the UI shows each camera's slug so the
// card config reads `camera: zimmer_ph`.

const liveKeySetting = "integrations.live_key"

// slugify turns a camera name into a stable, URL-safe handle: lowercase, runs of
// non-alphanumerics collapse to a single underscore, trimmed. "Zimmer PH" → "zimmer_ph".
func slugify(name string) string {
	var b strings.Builder
	underscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			underscore = false
			continue
		}
		if b.Len() > 0 && !underscore {
			b.WriteByte('_')
			underscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

// resolveCameraID maps a URL {id} that may be either a real camera id or a name-slug to
// the real id. The id path is an indexed lookup and the common case; the slug path only
// runs when that misses, so a first-party request (always a real id) pays nothing extra.
func (s *Server) resolveCameraID(idOrSlug string) (string, bool) {
	if idOrSlug == "" {
		return "", false
	}
	if _, err := s.st.GetCamera(idOrSlug); err == nil {
		return idOrSlug, true
	}
	cams, err := s.st.ListCameras()
	if err != nil {
		return "", false
	}
	want := slugify(idOrSlug)
	for _, c := range cams {
		if slugify(c.Name) == want {
			return c.ID, true
		}
	}
	return "", false
}

// currentLiveKey returns the managed live key, or "" if one has never been minted. Never
// creates one — the WS auth path must not have a side effect.
func (s *Server) currentLiveKey() string {
	v, _ := s.st.GetSetting(liveKeySetting)
	return v
}

// ensureLiveKey returns the managed live key, minting one on first use.
func (s *Server) ensureLiveKey() (string, error) {
	if v := s.currentLiveKey(); v != "" {
		return v, nil
	}
	return s.rollLiveKey()
}

// rollLiveKey generates a fresh live key and stores it, invalidating any previous value
// (the old key stops authorizing the moment it is overwritten).
func (s *Server) rollLiveKey() (string, error) {
	key := "aplv_" + store.NewID() + store.NewID()
	if err := s.st.SetSetting(liveKeySetting, key); err != nil {
		return "", err
	}
	return key, nil
}

// publicBaseURL reconstructs the URL the browser reached Aperture on (honouring a reverse
// proxy's forwarded scheme), so the UI can pre-fill the card's `url:` field.
func (s *Server) publicBaseURL(r *http.Request) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	return scheme + "://" + r.Host
}

func (s *Server) liveKeyView(r *http.Request, key string) map[string]any {
	cams, _ := s.st.ListCameras()
	list := make([]map[string]any, 0, len(cams))
	for _, c := range cams {
		list = append(list, map[string]any{"id": c.ID, "name": c.Name, "slug": slugify(c.Name)})
	}
	return map[string]any{"key": key, "cameras": list, "apertureUrl": s.publicBaseURL(r)}
}

// handleGetLiveKey returns the managed live key (minting it on first call) plus every
// camera's slug and a suggested base URL — everything the Live Card panel needs.
func (s *Server) handleGetLiveKey(w http.ResponseWriter, r *http.Request) {
	key, err := s.ensureLiveKey()
	if err != nil {
		s.log.Error("ensure live key", "err", err)
		writeError(w, http.StatusInternalServerError, "could not read live key")
		return
	}
	writeJSON(w, http.StatusOK, s.liveKeyView(r, key))
}

// handleRollLiveKey regenerates the live key. Existing cards must be updated with the new
// value; the old one is dead immediately.
func (s *Server) handleRollLiveKey(w http.ResponseWriter, r *http.Request) {
	key, err := s.rollLiveKey()
	if err != nil {
		s.log.Error("roll live key", "err", err)
		writeError(w, http.StatusInternalServerError, "could not roll live key")
		return
	}
	s.logAudit(r, "integrations.live_key.roll", "", "Home Assistant live key rerolled")
	writeJSON(w, http.StatusOK, s.liveKeyView(r, key))
}
