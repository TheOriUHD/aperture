package api

import (
	"net/http"
	"strings"

	"github.com/TheOriUHD/aperture/internal/eufy"
)

// Eufy bridge integration.
//
// Eufy publishes no local device API, so control goes through eufy-security-ws,
// which signs in to a Eufy cloud account. Two things about that server shape this
// code: it reads credentials only at startup (there is no runtime command to set
// them), and sign-in can raise a two-factor prompt or a captcha asynchronously.
// Both of those need somewhere to be answered, which is what these handlers are
// for — before this, the only way through was a shell.

const (
	eufyURLSetting     = "eufy_bridge_url"
	eufyUserSetting    = "eufy_user"
	eufyPassSetting    = "eufy_pass_enc"
	eufyCountrySetting = "eufy_country"

	// Where the bridge keeps its own config and what unit runs it. Constants rather
	// than settings: these describe how this host installs the bridge, not a user
	// preference, and making them editable would turn a settings form into a way to
	// point Aperture's root process at an arbitrary file.
	//
	// The config lives under /var/lib/aperture because that is the one tree
	// aperture.service can write (ProtectSystem=strict, ReadWritePaths=/var/lib/aperture).
	// Putting it in /etc would mean widening nvrd's write access to system config to
	// serve one integration, which is the wrong trade.
	eufyBridgeConfigPath = "/var/lib/aperture/eufy-bridge.json"
	eufyBridgeUnit       = "eufy-security-ws.service"

	eufyDefaultBridgeURL = "http://127.0.0.1:3000"
)

// eufyClient returns the shared bridge session, starting it on first use.
func (s *Server) eufyClient() *eufy.Client {
	s.eufyOnce.Do(func() {
		url, _ := s.st.GetSetting(eufyURLSetting)
		if strings.TrimSpace(url) == "" {
			url = eufyDefaultBridgeURL
		}
		s.eufyCli = eufy.New(eufy.Config{
			URL:        url,
			ConfigPath: eufyBridgeConfigPath,
			Unit:       eufyBridgeUnit,
		}, s.log)
		s.eufyCli.Start(s.eufyCtx)
		s.eufySyncConfigured()
	})
	return s.eufyCli
}

// eufySyncConfigured tells the client whether an account has been saved, so the UI
// can distinguish "never set up" from "set up but not connected".
func (s *Server) eufySyncConfigured() {
	user, _ := s.st.GetSetting(eufyUserSetting)
	enc, _ := s.st.GetSetting(eufyPassSetting)
	s.eufyCli.SetMarkConfigured(strings.TrimSpace(user) != "" && enc != "", user)
}

func (s *Server) handleGetEufy(w http.ResponseWriter, r *http.Request) {
	st := s.eufyClient().Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"configured":      st.Configured,
		"bridgeRunning":   st.BridgeRunning,
		"connected":       st.Connected,
		"needsVerifyCode": st.NeedsVerifyCode,
		"needsCaptcha":    st.NeedsCaptcha,
		"captchaId":       st.CaptchaID,
		"captchaImage":    st.CaptchaImage,
		"username":        st.Username,
		"lastError":       st.LastError,
		"devices":         st.Devices,
	})
}

// handleSetEufy saves the Eufy account and restarts the bridge so it takes effect.
func (s *Server) handleSetEufy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"` // empty = keep the stored one
		Country  string `json:"country"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		writeError(w, http.StatusBadRequest, "an account email is required")
		return
	}
	if s.cipher == nil {
		writeError(w, http.StatusInternalServerError, "secret storage is unavailable")
		return
	}

	// An empty password means "keep the one already stored", so the form can be
	// re-saved to change the country without retyping a password the UI never shows.
	password := req.Password
	if password == "" {
		enc, _ := s.st.GetSetting(eufyPassSetting)
		if enc == "" {
			writeError(w, http.StatusBadRequest, "a password is required the first time")
			return
		}
		plain, err := s.cipher.Decrypt(enc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read the stored password")
			return
		}
		password = plain
	}

	enc, err := s.cipher.Encrypt(password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not store the password")
		return
	}
	country := strings.ToUpper(strings.TrimSpace(req.Country))

	// Push to the bridge FIRST. If that fails there is no point recording an account
	// Aperture cannot actually use, and the user gets the real reason rather than a
	// success followed by a silently dead integration.
	if err := eufy.ApplyCredentials(eufy.Config{
		ConfigPath: eufyBridgeConfigPath,
		Unit:       eufyBridgeUnit,
	}, username, password, country); err != nil {
		s.log.Warn("eufy: applying credentials failed", "err", err)
		writeError(w, http.StatusBadGateway, "could not update the bridge: "+err.Error())
		return
	}

	_ = s.st.SetSetting(eufyUserSetting, username)
	_ = s.st.SetSetting(eufyPassSetting, enc)
	_ = s.st.SetSetting(eufyCountrySetting, country)
	s.eufyClient()
	s.eufySyncConfigured()
	s.logAudit(r, "integrations.eufy", username, "eufy account saved")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleEufyVerifyCode answers the emailed two-factor challenge.
func (s *Server) handleEufyVerifyCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		writeError(w, http.StatusBadRequest, "a verification code is required")
		return
	}
	if err := s.eufyClient().SetVerifyCode(r.Context(), code); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleEufyCaptcha answers a captcha challenge.
func (s *Server) handleEufyCaptcha(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CaptchaID string `json:"captchaId"`
		Code      string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, "a captcha answer is required")
		return
	}
	if err := s.eufyClient().SetCaptcha(r.Context(), req.CaptchaID, strings.TrimSpace(req.Code)); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
