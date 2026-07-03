package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/store"
)

// ---- API tokens (per-user) ------------------------------------------------

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	toks, err := s.st.ListAPITokens(c.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if toks == nil {
		toks = []store.APIToken{}
	}
	writeJSON(w, http.StatusOK, toks)
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	var req struct {
		Name          string `json:"name"`
		ExpiresInDays int    `json:"expiresInDays"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	var exp int64
	if req.ExpiresInDays > 0 {
		exp = time.Now().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour).UnixMilli()
	}
	tok, plain, err := s.st.CreateAPIToken(c.UserID, strings.TrimSpace(req.Name), exp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create token")
		return
	}
	s.logAudit(r, "token.create", tok.Name, "API token created")
	// Plaintext is returned exactly once.
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "secret": plain})
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	if err := s.st.DeleteAPIToken(chi.URLParam(r, "id"), c.UserID); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.logAudit(r, "token.revoke", chi.URLParam(r, "id"), "API token revoked")
	w.WriteHeader(http.StatusNoContent)
}

// ---- Webhooks (admin) -----------------------------------------------------

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	whs, err := s.st.ListWebhooks()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if whs == nil {
		whs = []store.Webhook{}
	}
	writeJSON(w, http.StatusOK, whs)
}

func (s *Server) decodeWebhook(r *http.Request) (*store.Webhook, error) {
	var wh store.Webhook
	if err := decodeJSON(r, &wh); err != nil {
		return nil, err
	}
	wh.Name = strings.TrimSpace(wh.Name)
	wh.URL = strings.TrimSpace(wh.URL)
	return &wh, nil
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	wh, err := s.decodeWebhook(r)
	if err != nil || wh.Name == "" || !strings.HasPrefix(wh.URL, "http") {
		writeError(w, http.StatusBadRequest, "name and a valid http(s) URL are required")
		return
	}
	if wh.Secret == "" {
		wh.Secret = store.NewID() + store.NewID() // random HMAC signing key
	}
	if err := s.st.CreateWebhook(wh); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create webhook")
		return
	}
	s.logAudit(r, "webhook.create", wh.Name, wh.URL)
	writeJSON(w, http.StatusOK, wh)
}

func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	wh, err := s.decodeWebhook(r)
	if err != nil || wh.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid webhook")
		return
	}
	wh.ID = chi.URLParam(r, "id")
	if err := s.st.UpdateWebhook(wh); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	s.logAudit(r, "webhook.update", wh.Name, wh.URL)
	writeJSON(w, http.StatusOK, wh)
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteWebhook(chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	wh, err := s.st.GetWebhook(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	code, err := s.webhook.TestFire(wh)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": code >= 200 && code < 300, "code": code})
}

func (s *Server) handleWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	ds, err := s.st.ListDeliveries(chi.URLParam(r, "id"), 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if ds == nil {
		ds = []store.WebhookDelivery{}
	}
	writeJSON(w, http.StatusOK, ds)
}
