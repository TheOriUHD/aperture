package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/auth"
)

// maxLeaseTTLSec caps a client-requested lease so a forgotten lease can't shield
// footage from retention indefinitely (matches the export lease bound).
const maxLeaseTTLSec = 2 * 60 * 60

// handleCreateLease takes a segment lease (T2.8) protecting [fromTs,toTs) of a
// camera's footage from retention for up to ttlSec (capped). A consumer doing
// long work over historical footage — an extension export or bulk download —
// takes one so the janitor won't prune footage mid-read; it is released
// explicitly or expires on its own.
func (s *Server) handleCreateLease(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	cameraID := chi.URLParam(r, "id")
	var req struct {
		FromTs int64 `json:"fromTs"`
		ToTs   int64 `json:"toTs"`
		TTLSec int64 `json:"ttlSec"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ToTs <= req.FromTs {
		writeErrorCode(w, http.StatusBadRequest, "validation", "fromTs and toTs are required and toTs must exceed fromTs")
		return
	}
	ttl := req.TTLSec
	if ttl <= 0 || ttl > maxLeaseTTLSec {
		ttl = maxLeaseTTLSec
	}
	expires := time.Now().Add(time.Duration(ttl) * time.Second).UnixMilli()
	l, err := s.st.CreateLease(cameraID, req.FromTs, req.ToTs, expires, c.Class+":"+c.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create lease")
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

// handleReleaseLease releases a held lease. Idempotent; scoped to the camera in
// the path so the release can't touch another camera's lease by a guessed id.
func (s *Server) handleReleaseLease(w http.ResponseWriter, r *http.Request) {
	leaseID := chi.URLParam(r, "leaseId")
	if l, err := s.st.GetLease(leaseID); err == nil && l.CameraID != chi.URLParam(r, "id") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.st.ReleaseLease(leaseID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not release lease")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
