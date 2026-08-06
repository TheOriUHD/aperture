package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/store"
)

func (s *Server) scheduleView(id string) []map[string]any {
	scheds, _ := s.st.ListSchedules(id)
	out := make([]map[string]any, 0, len(scheds))
	for _, sc := range scheds {
		out = append(out, map[string]any{
			"weekday":  sc.Weekday,
			"startMin": sc.StartMin,
			"endMin":   sc.EndMin,
		})
	}
	return out
}

func (s *Server) handleGetSchedule(w http.ResponseWriter, r *http.Request) {
	v := s.scheduleView(chi.URLParam(r, "id"))
	setETag(w, v)
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleSetSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.st.GetCamera(id); err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.precondition(w, r, etagOf(s.scheduleView(id))) {
		return
	}
	var req struct {
		Windows []struct {
			Weekday  int `json:"weekday"`
			StartMin int `json:"startMin"`
			EndMin   int `json:"endMin"`
		} `json:"windows"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	scheds := make([]*store.Schedule, 0, len(req.Windows))
	for _, win := range req.Windows {
		if win.Weekday < 0 || win.Weekday > 6 || win.EndMin <= win.StartMin {
			continue
		}
		scheds = append(scheds, &store.Schedule{
			Weekday: win.Weekday, StartMin: win.StartMin, EndMin: win.EndMin,
		})
	}
	if err := s.st.SetSchedules(id, scheds); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save schedule")
		return
	}
	s.rec.Reconcile() // apply scheduled windows immediately
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
