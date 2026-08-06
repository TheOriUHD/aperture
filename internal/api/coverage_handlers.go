package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/store"
)

// coverageRange is a merged interval of recorded coverage (unix ms). It mirrors
// the client-side Range in web/src/lib/playlist.ts so the two agree field-for-field.
type coverageRange struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

// mergeCoverage collapses start-ordered segments into coverage ranges, joining
// any two whose gap is <= toleranceMs. It is the Go port of mergeRanges()
// (web/src/lib/playlist.ts): at toleranceMs=2000 it reproduces the timeline rail
// exactly; toleranceMs=0 yields exact ranges (any gap at all starts a new range).
// segs must be ordered by start_ts — ListSegments already guarantees that, so no
// re-sort is needed (the client sorts only because its input order is unknown).
func mergeCoverage(segs []*store.Segment, toleranceMs int64) []coverageRange {
	out := make([]coverageRange, 0, len(segs))
	for _, seg := range segs {
		if n := len(out); n > 0 && seg.StartTS-out[n-1].End <= toleranceMs {
			if seg.EndTS > out[n-1].End {
				out[n-1].End = seg.EndTS
			}
			continue
		}
		out = append(out, coverageRange{Start: seg.StartTS, End: seg.EndTS})
	}
	return out
}

// handleCoverage returns a camera's recorded coverage over [from,to] as merged
// ranges, computed server-side. It replaces the timeline rail fetching every raw
// segment (~11,900 objects / 1.5 MB on this install) just to merge them client-
// side. tolerance_ms tunes gap-joining: default 2000 (rail parity), 0 = exact.
func (s *Server) handleCoverage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	from, to := parseRange(r)
	tolerance := int64(2000)
	if v := r.URL.Query().Get("tolerance_ms"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeErrorCode(w, http.StatusBadRequest, "validation", "tolerance_ms must be a non-negative integer")
			return
		}
		tolerance = n
	}
	segs, err := s.st.ListSegments(id, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, mergeCoverage(segs, tolerance))
}
