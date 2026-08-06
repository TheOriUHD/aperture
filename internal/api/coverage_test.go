package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestCoverageMergeAndTolerance covers T2.1: the coverage endpoint merges
// segments into ranges server-side, and tolerance_ms controls gap-joining
// (2000 = rail parity, 0 = exact) exactly like the client mergeRanges.
func TestCoverageMergeAndTolerance(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	const T = int64(1_700_000_000_000)
	// A,B contiguous; C 1s after B; D 9s after C.
	for _, seg := range []struct{ start, end int64 }{
		{T, T + 10_000},
		{T + 10_000, T + 20_000},
		{T + 21_000, T + 31_000},
		{T + 40_000, T + 50_000},
	} {
		mustDo(t, s.st.InsertSegment(&store.Segment{
			CameraID: "cam1", Path: "x", StartTS: seg.start, EndTS: seg.end,
			DurationMS: seg.end - seg.start,
		}))
	}

	get := func(tol string) []coverageRange {
		t.Helper()
		path := fmt.Sprintf("/api/v1/cameras/cam1/coverage?from=%d&to=%d&tolerance_ms=%s", T-1_000, T+60_000, tol)
		rec := s.reqAs("GET", path, adminC)
		if rec.Code != http.StatusOK {
			t.Fatalf("coverage tol=%s: got %d, want 200 (%s)", tol, rec.Code, rec.Body.String())
		}
		var out []coverageRange
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	// tolerance 2000: A+B+C merge (gaps 0 and 1000 <= 2000); D stands alone.
	if got := get("2000"); len(got) != 2 ||
		got[0] != (coverageRange{T, T + 31_000}) || got[1] != (coverageRange{T + 40_000, T + 50_000}) {
		t.Fatalf("tol=2000 ranges = %+v, want [{%d %d} {%d %d}]", got, T, T+31_000, T+40_000, T+50_000)
	}

	// tolerance 0: only the exactly-contiguous A+B merge; C and D separate.
	if got := get("0"); len(got) != 3 ||
		got[0] != (coverageRange{T, T + 20_000}) ||
		got[1] != (coverageRange{T + 21_000, T + 31_000}) ||
		got[2] != (coverageRange{T + 40_000, T + 50_000}) {
		t.Fatalf("tol=0 ranges = %+v, want 3 exact ranges", got)
	}
}

// TestCoverageDefaultsAndValidation covers the default tolerance (2000) and the
// rejection of a negative tolerance_ms.
func TestCoverageDefaultsAndValidation(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	const T = int64(1_700_000_000_000)
	mustDo(t, s.st.InsertSegment(&store.Segment{CameraID: "cam1", Path: "x", StartTS: T, EndTS: T + 10_000}))
	mustDo(t, s.st.InsertSegment(&store.Segment{CameraID: "cam1", Path: "x", StartTS: T + 11_000, EndTS: T + 21_000}))

	// Default (no tolerance_ms) == 2000: the 1s gap joins into a single range.
	rec := s.reqAs("GET", fmt.Sprintf("/api/v1/cameras/cam1/coverage?from=%d&to=%d", T-1_000, T+30_000), adminC)
	if rec.Code != http.StatusOK {
		t.Fatalf("default tolerance: got %d, want 200", rec.Code)
	}
	var out []coverageRange
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &out))
	if len(out) != 1 || out[0] != (coverageRange{T, T + 21_000}) {
		t.Fatalf("default tolerance ranges = %+v, want one merged [{%d %d}]", out, T, T+21_000)
	}

	// Negative tolerance_ms → 400 validation.
	rec = s.reqAs("GET", "/api/v1/cameras/cam1/coverage?tolerance_ms=-5", adminC)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative tolerance_ms: got %d, want 400", rec.Code)
	}
}

// TestMergeCoverageUnit checks the merge helper's edge cases directly.
func TestMergeCoverageUnit(t *testing.T) {
	if got := mergeCoverage(nil, 2000); len(got) != 0 {
		t.Errorf("empty input = %+v, want empty (non-nil) slice", got)
	}
	// An overlapping segment must not shrink the range end.
	segs := []*store.Segment{
		{StartTS: 100, EndTS: 500},
		{StartTS: 200, EndTS: 300}, // fully inside the first
	}
	got := mergeCoverage(segs, 0)
	if len(got) != 1 || got[0].End != 500 {
		t.Errorf("overlap merge = %+v, want single range ending at 500", got)
	}
}
