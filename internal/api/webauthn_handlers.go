package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/store"
)

const waLoginCookie = "aperture_wa"

// --- pending-challenge store (single node, in-memory, short TTL) ----------

type waSessions struct {
	mu sync.Mutex
	m  map[string]waSessEntry
}

type waSessEntry struct {
	data webauthn.SessionData
	exp  time.Time
}

func newWASessions() *waSessions { return &waSessions{m: map[string]waSessEntry{}} }

func (s *waSessions) put(key string, d *webauthn.SessionData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, e := range s.m {
		if e.exp.Before(now) {
			delete(s.m, k)
		}
	}
	s.m[key] = waSessEntry{data: *d, exp: now.Add(5 * time.Minute)}
}

func (s *waSessions) take(key string) (webauthn.SessionData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if ok {
		delete(s.m, key)
	}
	if !ok || e.exp.Before(time.Now()) {
		return webauthn.SessionData{}, false
	}
	return e.data, true
}

// --- webauthn.User adapter -----------------------------------------------

type waUser struct {
	u     *store.User
	creds []webauthn.Credential
}

func (w *waUser) WebAuthnID() []byte   { return []byte(w.u.ID) }
func (w *waUser) WebAuthnName() string { return w.u.Email }
func (w *waUser) WebAuthnDisplayName() string {
	if w.u.Name != "" {
		return w.u.Name
	}
	return w.u.Email
}
func (w *waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

func (s *Server) loadWAUser(u *store.User) (*waUser, error) {
	stored, err := s.st.ListWebAuthnCredentials(u.ID)
	if err != nil {
		return nil, err
	}
	creds := make([]webauthn.Credential, 0, len(stored))
	for _, c := range stored {
		var cred webauthn.Credential
		if json.Unmarshal([]byte(c.CredJSON), &cred) == nil {
			creds = append(creds, cred)
		}
	}
	return &waUser{u: u, creds: creds}, nil
}

// webAuthn builds a relying-party config from the request host, so passkeys work
// on localhost and on any domain the UI is served from without extra config.
func (s *Server) webAuthn(r *http.Request) (*webauthn.WebAuthn, error) {
	host := r.Host
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	rpid := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		rpid = h
	}
	return webauthn.New(&webauthn.Config{
		RPDisplayName: "Aperture NVR",
		RPID:          rpid,
		RPOrigins:     []string{scheme + "://" + host},
	})
}

func newToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func excludeList(creds []webauthn.Credential) []protocol.CredentialDescriptor {
	out := make([]protocol.CredentialDescriptor, 0, len(creds))
	for _, c := range creds {
		out = append(out, c.Descriptor())
	}
	return out
}

// --- registration (authenticated) ----------------------------------------

func (s *Server) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	u, err := s.st.GetUserByID(c.UserID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	wa, err := s.webAuthn(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	wu, err := s.loadWAUser(u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	opts, sd, err := wa.BeginRegistration(wu,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementPreferred,
			UserVerification: protocol.VerificationPreferred,
		}),
		webauthn.WithExclusions(excludeList(wu.creds)),
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not start passkey setup")
		return
	}
	s.waSess.put("reg:"+u.ID, sd)
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	u, err := s.st.GetUserByID(c.UserID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	sd, ok := s.waSess.take("reg:" + u.ID)
	if !ok {
		writeError(w, http.StatusBadRequest, "passkey setup expired — try again")
		return
	}
	wa, err := s.webAuthn(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	wu, err := s.loadWAUser(u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	cred, err := wa.FinishRegistration(wu, sd, r)
	if err != nil {
		s.log.Warn("passkey register", "err", err)
		writeError(w, http.StatusBadRequest, "could not register passkey")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = "Passkey"
	}
	credJSON, _ := json.Marshal(cred)
	credID := base64.RawURLEncoding.EncodeToString(cred.ID)
	if err := s.st.AddWebAuthnCredential(u.ID, name, credID, string(credJSON)); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save passkey")
		return
	}
	s.logAudit(r, "auth.passkey_added", u.Email, name)
	writeJSON(w, http.StatusCreated, map[string]any{"status": "ok"})
}

// --- passwordless login (public) -----------------------------------------

func (s *Server) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	wa, err := s.webAuthn(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	opts, sd, err := wa.BeginDiscoverableLogin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start passkey sign-in")
		return
	}
	sid := newToken()
	s.waSess.put("login:"+sid, sd)
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name: waLoginCookie, Value: sid, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: secure, MaxAge: 300,
	})
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	ck, err := r.Cookie(waLoginCookie)
	if err != nil {
		writeError(w, http.StatusBadRequest, "sign-in expired — try again")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: waLoginCookie, Value: "", Path: "/", MaxAge: -1})
	sd, ok := s.waSess.take("login:" + ck.Value)
	if !ok {
		writeError(w, http.StatusBadRequest, "sign-in expired — try again")
		return
	}
	wa, err := s.webAuthn(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var matched *store.User
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := s.st.GetUserByID(string(userHandle))
		if err != nil {
			return nil, err
		}
		if u.Disabled {
			return nil, errors.New("account disabled")
		}
		matched = u
		return s.loadWAUser(u)
	}
	_, cred, err := wa.FinishPasskeyLogin(handler, sd, r)
	if err != nil || matched == nil {
		s.logAuditAs(r, "passkey", "auth.login_failed", "", "passkey")
		writeError(w, http.StatusUnauthorized, "passkey sign-in failed")
		return
	}
	if cred != nil {
		credJSON, _ := json.Marshal(cred)
		_ = s.st.UpdateWebAuthnCredential(base64.RawURLEncoding.EncodeToString(cred.ID), string(credJSON))
	}
	// A passkey is a second factor in itself: remember the browser, so a later
	// password sign-in from it skips the authenticator code.
	if err := s.startSession(w, r, matched, s.trustDevice(w, r, matched)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.logAuditAs(r, matched.Email, "auth.login", matched.Email, "passkey")
	writeJSON(w, http.StatusOK, s.userPayload(matched))
}

// --- management (authenticated) ------------------------------------------

func (s *Server) handleListPasskeys(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	creds, err := s.st.ListWebAuthnCredentials(c.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if creds == nil {
		creds = []store.WebAuthnCred{}
	}
	writeJSON(w, http.StatusOK, creds)
}

func (s *Server) handleDeletePasskey(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	id := chi.URLParam(r, "id")
	if err := s.st.DeleteWebAuthnCredential(id, c.UserID); err != nil {
		writeStoreError(w, err)
		return
	}
	s.logAudit(r, "auth.passkey_removed", c.Email, id)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
