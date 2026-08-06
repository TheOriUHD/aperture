package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/auth"
)

// brokerRequest is what a UI extension's SDK asks the host to perform. The host
// (the web app, a first-party session) relays it here; the extension itself is a
// sandboxed iframe with no cookies and no network to Aperture.
type brokerRequest struct {
	Method string          `json:"method"`
	Path   string          `json:"path"` // relative to /api/v1, e.g. "/cameras/123/events"
	Body   json.RawMessage `json:"body,omitempty"`
}

var brokerMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true,
}

// handleExtBroker executes an extension's intended API call under a synthesized
// identity that is the INTERSECTION of the extension's manifest authority and the
// CALLING session's own authority (DD-BROKER / R1). The synthesized claim carries
// the caller's UserID + Role (so admin-gated routes need the *caller* to be admin,
// and camera grants compose against the *caller's* ACL) but the extension's scopes
// and camera grants (so it can never exceed its manifest). It is re-dispatched
// through the real router, reusing enforce/allowedCameraSet unchanged — no token
// ever reaches the browser, and a UI extension can never do more for a user than
// that user could do unaided.
func (s *Server) handleExtBroker(w http.ResponseWriter, r *http.Request) {
	caller := auth.FromContext(r.Context()) // the first-party session (authedSessionOnly)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !s.extEnabled() {
		writeError(w, http.StatusNotFound, "extensions are disabled")
		return
	}
	ex, err := s.st.GetExtension(chi.URLParam(r, "id"))
	if err != nil || !ex.Enabled {
		writeError(w, http.StatusNotFound, "extension not found or disabled")
		return
	}

	var req brokerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	method := strings.ToUpper(req.Method)
	if !brokerMethods[method] {
		writeErrorCode(w, http.StatusBadRequest, "validation", "unsupported method")
		return
	}
	// The path is relative to /api/v1. Reject anything that could escape it or
	// re-enter the alias surface.
	if !strings.HasPrefix(req.Path, "/") || strings.HasPrefix(req.Path, "/api") || strings.Contains(req.Path, "..") {
		writeErrorCode(w, http.StatusBadRequest, "validation", "path must be relative to /api/v1 (e.g. /cameras)")
		return
	}

	claim := &auth.SessionClaims{
		UserID:       caller.UserID, // caller identity → caller's camera ACL + role gate
		Role:         caller.Role,
		Class:        auth.ClassToken,   // so scope + sessionOnly enforcement applies
		Scopes:       ex.Scopes,         // extension's manifest scopes (its ceiling)
		CameraGrants: ex.CameraGrants,   // composed as caller-ACL ∩ extension-grants
	}

	var body *bytes.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	} else {
		body = bytes.NewReader(nil)
	}
	sub, err := http.NewRequestWithContext(auth.WithClaims(context.Background(), claim), method, "/api/v1"+req.Path, body)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation", "invalid path")
		return
	}
	sub.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, sub) // re-enter the router; am.Context is a no-op (no cookie), so the synth claim stands

	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

// csrfGuard blocks a cross-site cookie-authenticated write. It matters only for
// session (cookie) credentials — the classic CSRF target; Bearer tokens (nvrctl,
// backend extensions) carry no cookie and are exempt, and the brokered sub-request
// is class=token so it is exempt too. A PRESENT-and-foreign Origin (or Referer,
// when Origin is absent) is rejected; a MISSING one is allowed, because nvrctl and
// curl deploy-checks send none and only a browser attaches Origin to a cross-site
// POST (R5). Do not "harden" the missing-Origin case into a rejection — it breaks
// nvrctl. The session cookie is also SameSite=Lax (auth.go); this is defence in depth.
func (s *Server) csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if c := auth.FromContext(r.Context()); c != nil && c.Class == auth.ClassSession {
				if o := requestOrigin(r); o != "" && !sameOrigin(o, r.Host) {
					writeErrorCode(w, http.StatusForbidden, "forbidden", "cross-origin request blocked")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func requestOrigin(r *http.Request) string {
	if o := r.Header.Get("Origin"); o != "" {
		return o
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		if u, err := url.Parse(ref); err == nil && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	return "" // absent → allowed (non-browser clients)
}

func sameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Host == host
}
