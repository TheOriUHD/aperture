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
		"seq":      e.Seq, // monotonic cursor for /events + SSE Last-Event-ID
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
	if e.TopLabel != "" {
		v["topLabel"] = e.TopLabel
	}
	if e.ObjectSummary != "" {
		v["objectSummary"] = e.ObjectSummary
	}
	if len(e.Objects) > 0 {
		v["objects"] = e.Objects
	}
	if len(e.MotionCells) > 0 {
		v["motionCells"] = e.MotionCells
	}
	return v
}

// attachObjects batch-loads detection boxes for a set of events (one query) so
// the list surfaces can draw labels/boxes without an N+1 fan-out.
func (s *Server) attachObjects(evs []*store.Event) {
	ids := make([]string, len(evs))
	for i, e := range evs {
		ids[i] = e.ID
	}
	objs, err := s.st.EventObjectsFor(ids)
	if err != nil {
		return
	}
	for _, e := range evs {
		if o := objs[e.ID]; len(o) > 0 {
			e.Objects = o
		}
	}
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
	s.attachObjects(evs)
	enr := s.newCondEnricher()
	out := make([]map[string]any, 0, len(evs))
	for _, e := range evs {
		v := eventView(e, now)
		if c := enr.conditions(e.StartTS); c != nil {
			v["conditions"] = c
		}
		out = append(out, v)
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
	isAdmin, allowed := s.allowedCameraSet(r)
	fetch := limit
	if !isAdmin {
		fetch = limit * 4 // over-fetch, then filter to the viewer's cameras
		if fetch > 1000 {
			fetch = 1000
		}
	}
	evs, err := s.st.ListEventsAll(from, to, fetch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	now := time.Now().UnixMilli()
	s.attachObjects(evs)
	enr := s.newCondEnricher()
	out := make([]map[string]any, 0, len(evs))
	for _, e := range evs {
		if !isAdmin && !allowed[e.CameraID] {
			continue
		}
		if len(out) >= limit {
			break
		}
		v := eventView(e, now)
		if c := enr.conditions(e.StartTS); c != nil {
			v["conditions"] = c
		}
		out = append(out, v)
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
	if !s.allowedCamera(r, e.CameraID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, e.ThumbPath)
}
