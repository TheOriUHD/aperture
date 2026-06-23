package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/stream"
)

// streamTokenTTL bounds how long a minted token can be used to open a live
// connection. The WebSocket itself may stay open longer once established.
const streamTokenTTL = 5 * time.Minute

// handleStreamToken mints a short-lived token the browser uses to open the live
// WebSocket for a camera without exposing camera credentials.
func (s *Server) handleStreamToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.st.GetCamera(id); err != nil {
		writeStoreError(w, err)
		return
	}
	token, err := s.am.StreamToken(id, streamTokenTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"wsUrl": "/api/cameras/" + id + "/ws?token=" + token,
	})
}

// handleStreamWS authorizes a stream token, then proxies the live WebSocket
// through to go2rtc (WebRTC with MSE fallback).
func (s *Server) handleStreamWS(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	token := r.URL.Query().Get("token")
	camID, err := s.am.VerifyStreamToken(token)
	if err != nil || camID != id {
		writeError(w, http.StatusUnauthorized, "invalid stream token")
		return
	}
	s.sm.ProxyWS(w, r, stream.StreamName(id))
}
