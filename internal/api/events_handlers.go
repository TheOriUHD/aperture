package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/store"
)

func eventView(e *store.Event, now int64) map[string]any {
	end := e.EndTS
	if end == 0 {
		end = now // ongoing
	}
	// bug 4.6: the v1 contract exposes endTsMs = null while an event is ongoing,
	// added alongside the legacy endTs (which stays `now` so the current web app,
	// still on /api, keeps drawing the live bar until it adopts endTsMs in 5.2).
	var endTsMs any
	if e.EndTS != 0 {
		endTsMs = e.EndTS
	}
	v := map[string]any{
		"id":       e.ID,
		"seq":      e.Seq, // monotonic cursor for /events + SSE Last-Event-ID
		"cameraId": e.CameraID,
		"type":     e.Type,
		"startTs":  e.StartTS,
		"endTs":    end,
		"endTsMs":  endTsMs,
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
	limit := pageLimit(r, 200) // default 200 for the alarm feed; convention max 1000
	beforeSeq, present, ok := cursorInt(r)
	if present && !ok {
		writeErrorCode(w, http.StatusBadRequest, "validation", "invalid cursor")
		return
	}
	// Camera ACL is pushed into SQL (T2.7): admins see all (nil filter); a viewer
	// or scoped token is restricted to its camera set, so the query scans at most
	// `limit` of the caller's own rows instead of over-fetching and dropping.
	isAdmin, allowed := s.allowedCameraSet(r)
	var cams []string
	if !isAdmin {
		cams = make([]string, 0, len(allowed))
		for id := range allowed {
			cams = append(cams, id)
		}
	}
	evs, err := s.st.ListEventsAllScoped(from, to, beforeSeq, limit, cams)
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
	// Keyset cursor (T2.4): the last row has the smallest seq on this page.
	if n := len(evs); n > 0 {
		setNextCursor(w, n, limit, evs[n-1].Seq)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetEvent returns a single event as an addressable v1 resource. events:read;
// the camera is resolved from the event row and checked against the caller's ACL.
func (s *Server) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	e, err := s.st.EventByID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.allowedCamera(r, e.CameraID) {
		writeErrorCode(w, http.StatusForbidden, "forbidden.camera", "access denied")
		return
	}
	s.attachObjects([]*store.Event{e})
	writeJSON(w, http.StatusOK, eventView(e, time.Now().UnixMilli()))
}

// handleEventDetections returns an event's detection objects, each addressable
// by its stable id (T1.6). events:read; camera resolved from the event row.
func (s *Server) handleEventDetections(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	e, err := s.st.EventByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.allowedCamera(r, e.CameraID) {
		writeErrorCode(w, http.StatusForbidden, "forbidden.camera", "access denied")
		return
	}
	objs, _ := s.st.EventObjectsFor([]string{id})
	out := objs[id]
	if out == nil {
		out = []store.EventObject{}
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
