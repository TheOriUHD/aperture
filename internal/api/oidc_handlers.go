package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/TheOriUHD/aperture/internal/store"
)

const oidcStateCookie = "aperture_oidc_state"

// handleOIDCStatus is public so the login page can show the SSO button.
func (s *Server) handleOIDCStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": s.oidc.Enabled(),
		"name":    s.oidc.DisplayName(),
	})
}

// handleOIDCLogin starts the SSO flow.
func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	state := randState()
	http.SetCookie(w, &http.Cookie{
		Name: oidcStateCookie, Value: state, Path: "/",
		HttpOnly: true, Secure: strings.HasPrefix(s.cfg.PublicURL, "https://"),
		SameSite: http.SameSiteLaxMode, MaxAge: 300,
	})
	url, err := s.oidc.AuthCodeURL(r.Context(), state)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "SSO is not available")
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

// handleOIDCCallback completes the flow, signs the user in, and redirects home.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	fail := func() { http.Redirect(w, r, "/?error=sso", http.StatusFound) }

	c, err := r.Cookie(oidcStateCookie)
	if err != nil || c.Value == "" || c.Value != r.URL.Query().Get("state") {
		fail()
		return
	}
	claims, err := s.oidc.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil || claims.Email == "" {
		s.log.Warn("oidc callback", "err", err)
		fail()
		return
	}
	if dom := s.oidc.LoadSettings().AllowedDomain; dom != "" && !strings.HasSuffix(claims.Email, "@"+dom) {
		fail()
		return
	}

	u, err := s.st.GetUserByOIDCSub(claims.Sub)
	if err != nil {
		// Not linked yet — try to attach to an existing local account by email.
		if existing, e2 := s.st.GetUserByEmail(claims.Email); e2 == nil {
			_ = s.st.SetUserOIDC(existing.ID, claims.Sub)
			u = existing
		} else if s.oidc.LoadSettings().AutoCreate {
			sub := claims.Sub
			nu := &store.User{Email: claims.Email, Name: claims.Name, Role: store.RoleViewer, OIDCSub: &sub}
			if err := s.st.CreateUser(nu); err != nil {
				fail()
				return
			}
			u = nu
		} else {
			fail()
			return
		}
	}
	if u.Disabled {
		fail()
		return
	}
	if err := s.startSession(w, r, u, s.trustedDevice(r, u.ID)); err != nil {
		fail()
		return
	}
	s.logAuditAs(r, u.Email, "auth.login", u.Email, "sso")
	http.Redirect(w, r, "/", http.StatusFound)
}

func randState() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- OIDC settings (admin) ----------------------------------------------

func (s *Server) handleGetOIDC(w http.ResponseWriter, r *http.Request) {
	set := s.oidc.LoadSettings()
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":     set.Enabled,
		"issuer":      set.Issuer,
		"clientId":    set.ClientID,
		"domain":      set.AllowedDomain,
		"autoCreate":  set.AutoCreate,
		"name":        set.DisplayName,
		"hasSecret":   set.ClientSecret != "",
		"redirectUrl": s.oidc.RedirectURL(),
	})
}

func (s *Server) handleSetOIDC(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled    bool   `json:"enabled"`
		Issuer     string `json:"issuer"`
		ClientID   string `json:"clientId"`
		Secret     string `json:"clientSecret"` // empty = keep
		Domain     string `json:"domain"`
		AutoCreate bool   `json:"autoCreate"`
		Name       string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	_ = s.st.SetSetting("oidc_enabled", boolStr(req.Enabled))
	_ = s.st.SetSetting("oidc_issuer", strings.TrimRight(strings.TrimSpace(req.Issuer), "/"))
	_ = s.st.SetSetting("oidc_client_id", strings.TrimSpace(req.ClientID))
	_ = s.st.SetSetting("oidc_domain", strings.TrimSpace(req.Domain))
	_ = s.st.SetSetting("oidc_autocreate", boolStr(req.AutoCreate))
	_ = s.st.SetSetting("oidc_name", strings.TrimSpace(req.Name))
	if req.Secret != "" {
		_ = s.st.SetSetting("oidc_client_secret", req.Secret)
	}
	s.logAudit(r, "oidc.update", "", "enabled="+boolStr(req.Enabled))
	s.handleGetOIDC(w, r)
}
