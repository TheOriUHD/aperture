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
	cam, err := s.st.GetCamera(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	token, err := s.am.StreamToken(id, streamTokenTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// hasSub tells the client whether a low-quality stream exists, so it can offer
	// the Low quality option. The client appends &q=sub to wsUrl to request it.
	writeJSON(w, http.StatusOK, map[string]any{
		"token":  token,
		"wsUrl":  "/api/cameras/" + id + "/ws?token=" + token,
		"hasSub": cam.RTSPSubURL != "",
	})
}

// handleSnapshot returns a single JPEG frame for a camera (used for thumbnails).
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.st.GetCamera(id); err != nil {
		writeStoreError(w, err)
		return
	}
	data, err := s.sm.SnapshotCached(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot unavailable")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
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
	// Quality selection: q=sub proxies the low-quality stream when the camera has
	// one; otherwise fall back to the main stream.
	name := stream.StreamName(id)
	if r.URL.Query().Get("q") == "sub" {
		if cam, err := s.st.GetCamera(id); err == nil && cam.RTSPSubURL != "" {
			name = stream.SubStreamName(id)
		}
	}
	s.sm.ProxyWS(w, r, name)
}
