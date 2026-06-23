package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/store"
)

func eventView(e *store.Event, now int64) map[string]any {
	end := e.EndTS
	if end == 0 {
		end = now // ongoing
	}
	v := map[string]any{
		"id":       e.ID,
		"cameraId": e.CameraID,
		"type":     e.Type,
		"startTs":  e.StartTS,
		"endTs":    end,
		"ongoing":  e.EndTS == 0,
		"score":    e.Score,
		"reviewed": e.Reviewed,
		"saved":    e.Saved,
	}
	if e.ThumbPath != "" {
		v["thumbUrl"] = "/api/events/" + e.ID + "/thumb.jpg"
	}
	return v
}

// handleCameraEvents lists motion events for a camera in a time range.
func (s *Server) handleCameraEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	from, to := parseRange(r)
	evs, err := s.st.ListEvents(id, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	now := time.Now().UnixMilli()
	out := make([]map[string]any, 0, len(evs))
	for _, e := range evs {
		out = append(out, eventView(e, now))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleEventsAll lists recent events across all cameras (for the Alarms feed).
func (s *Server) handleEventsAll(w http.ResponseWriter, r *http.Request) {
	from, to := parseRange(r)
	limit := 200
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 1000 {
		limit = v
	}
	evs, err := s.st.ListEventsAll(from, to, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	now := time.Now().UnixMilli()
	out := make([]map[string]any, 0, len(evs))
	for _, e := range evs {
		out = append(out, eventView(e, now))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleEventThumb serves an event's thumbnail image.
func (s *Server) handleEventThumb(w http.ResponseWriter, r *http.Request) {
	e, err := s.st.EventByID(chi.URLParam(r, "id"))
	if err != nil || e.ThumbPath == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, e.ThumbPath)
}
