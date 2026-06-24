package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/store"
)

// logAudit records an action attributed to the request's session.
func (s *Server) logAudit(r *http.Request, action, target, detail string) {
	if s.audit != nil {
		s.audit.Record(r, action, target, detail)
	}
}

// logAuditAs records an action with an explicit actor (pre-session events).
func (s *Server) logAuditAs(r *http.Request, actor, action, target, detail string) {
	if s.audit != nil {
		s.audit.RecordAs(r, actor, action, target, detail)
	}
}

// userPayload is the /me-style response: public profile + the user's prefs.
func (s *Server) userPayload(u *store.User) map[string]any {
	m := publicUser(u)
	if p, err := s.st.GetPrefs(u.ID); err == nil {
		m["preferences"] = json.RawMessage(p)
	} else {
		m["preferences"] = json.RawMessage("{}")
	}
	m["mustChangePassword"] = u.MustChangePassword
	m["twoFactor"] = u.TOTPEnabled
	n, _ := s.st.CountWebAuthnCredentials(u.ID)
	m["passkeys"] = n
	return m
}

// --- preferences ---------------------------------------------------------

func (s *Server) handleGetPrefs(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	p, err := s.st.GetPrefs(c.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(p))
}

func (s *Server) handleSetPrefs(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	var obj map[string]any
	if err := decodeJSON(r, &obj); err != nil {
		writeError(w, http.StatusBadRequest, "preferences must be a JSON object")
		return
	}
	b, _ := json.Marshal(obj)
	if len(b) > 64*1024 {
		writeError(w, http.StatusBadRequest, "preferences too large")
		return
	}
	if err := s.st.SetPrefs(c.UserID, string(b)); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save preferences")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// --- audit log -----------------------------------------------------------

func (s *Server) handleGetAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.AuditFilter{
		Action: q.Get("action"),
		Actor:  q.Get("actor"),
		Limit:  atoiOr(q.Get("limit"), 200),
		Offset: atoiOr(q.Get("offset"), 0),
	}
	if v, err := strconv.ParseInt(q.Get("from"), 10, 64); err == nil {
		f.From = v
	}
	if v, err := strconv.ParseInt(q.Get("to"), 10, 64); err == nil {
		f.To = v
	}
	entries, err := s.st.ListAudit(f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	actions, _ := s.st.DistinctAuditActions()
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "actions": actions})
}

func (s *Server) handleVerifyAudit(w http.ResponseWriter, r *http.Request) {
	bad, err := s.st.VerifyAuditChain()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"intact": bad == 0, "firstTamperedId": bad})
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}
