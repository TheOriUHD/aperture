package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Camera ⇄ storage destinations: a camera records to ONE primary destination
// (ffmpeg's target) and may MIRROR to any number of others, each with its own
// retention. See migration 0030 and internal/record/mirror.go.

func (s *Server) handleGetCameraDestinations(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cam, err := s.st.GetCamera(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	dests, err := s.st.DestinationsForCamera(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(dests))
	for _, d := range dests {
		out = append(out, map[string]any{
			"locationId":    d.LocationID,
			"retentionDays": d.RetentionDays,
			"primary":       d.LocationID == cam.LocationID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"cameraId": id, "destinations": out})
}

func (s *Server) handleSetCameraDestinations(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cam, err := s.st.GetCamera(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var req struct {
		PrimaryLocationID string `json:"primaryLocationId"`
		Destinations      []struct {
			LocationID    string `json:"locationId"`
			RetentionDays int    `json:"retentionDays"`
		} `json:"destinations"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	if len(req.Destinations) == 0 {
		writeErrorCode(w, http.StatusBadRequest, "validation", "a camera needs at least one destination")
		return
	}
	// Every named location must exist (or be the default) and be distinct.
	seen := map[string]bool{}
	dests := make([]store.CameraDestination, 0, len(req.Destinations))
	primaryListed := false
	for _, d := range req.Destinations {
		if seen[d.LocationID] {
			writeErrorCode(w, http.StatusBadRequest, "validation", "duplicate destination")
			return
		}
		seen[d.LocationID] = true
		if d.LocationID != store.DefaultLocationID {
			if _, err := s.st.GetStorageLocation(d.LocationID); err != nil {
				writeErrorCode(w, http.StatusBadRequest, "validation", "unknown location")
				return
			}
		}
		if d.LocationID == req.PrimaryLocationID {
			primaryListed = true
		}
		dests = append(dests, store.CameraDestination{
			CameraID: id, LocationID: d.LocationID, RetentionDays: d.RetentionDays,
		})
	}
	if !primaryListed {
		writeErrorCode(w, http.StatusBadRequest, "validation", "the primary must be one of the destinations")
		return
	}
	primaryChanged := cam.LocationID != req.PrimaryLocationID
	if err := s.st.SetCameraDestinations(id, req.PrimaryLocationID, dests); err != nil {
		s.log.Error("set camera destinations", "camera", id, "err", err)
		writeError(w, http.StatusInternalServerError, "could not save destinations")
		return
	}
	// A primary change moves ffmpeg's write target — restart that one recorder
	// (the single counted restart pattern; other cameras are untouched). Mirrors
	// need no restart at all: the mirror pass picks them up on its next tick.
	if primaryChanged {
		s.rec.RestartCamera(id)
	}
	s.logAudit(r, "storage.destinations", id, "")
	s.emitStorage()
	s.handleGetCameraDestinations(w, r)
}
