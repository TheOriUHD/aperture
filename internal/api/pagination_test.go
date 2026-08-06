package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestCursorPrimitives covers the opaque-cursor helpers.
func TestCursorPrimitives(t *testing.T) {
	tok := encodeCursor("12345")
	if tok == "12345" {
		t.Errorf("cursor should be encoded, not the raw key")
	}
	if p, ok := decodeCursor(tok); !ok || p != "12345" {
		t.Errorf("round-trip = (%q,%v), want (\"12345\",true)", p, ok)
	}
	if _, ok := decodeCursor("!!!not-base64!!!"); ok {
		t.Errorf("malformed token decoded ok")
	}
}

// TestPageLimitClamp covers ?limit parsing and clamping.
func TestPageLimitClamp(t *testing.T) {
	mk := func(q string) *http.Request { return httptest.NewRequest("GET", "/x?"+q, nil) }
	cases := []struct {
		q    string
		def  int
		want int
	}{
		{"", 200, 200},          // absent → default
		{"limit=50", 200, 50},   // in range
		{"limit=0", 200, 200},   // non-positive → default
		{"limit=-5", 200, 200},  // negative → default
		{"limit=abc", 200, 200}, // junk → default
		{"limit=99999", 200, maxPageLimit},
		{"", 0, defaultPageLimit}, // def<=0 → convention default
	}
	for _, c := range cases {
		if got := pageLimit(mk(c.q), c.def); got != c.want {
			t.Errorf("pageLimit(%q, def=%d) = %d, want %d", c.q, c.def, got, c.want)
		}
	}
}

// TestEventsKeysetNoDupNoSkip covers T2.4: paging /events by the seq keyset
// cursor returns every row exactly once even when a new event is inserted
// between page fetches — the property offset paging lacks.
func TestEventsKeysetNoDupNoSkip(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	const T = int64(1_700_000_000_000)
	for i := 0; i < 10; i++ { // seq 1..10
		mustDo(t, s.st.InsertEvent(&store.Event{CameraID: "cam1", StartTS: T + int64(i)}))
	}

	rng := fmt.Sprintf("from=%d&to=%d", T-1, T+1_000_000)
	seen := map[int64]int{}
	cursor := ""
	pages := 0
	for {
		url := fmt.Sprintf("/api/v1/events?%s&limit=4", rng)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		rec := s.reqAs("GET", url, adminC)
		if rec.Code != http.StatusOK {
			t.Fatalf("page %d: got %d, want 200", pages, rec.Code)
		}
		var out []map[string]any
		mustDo(t, json.Unmarshal(rec.Body.Bytes(), &out))
		for _, e := range out {
			seen[int64(e["seq"].(float64))]++
		}
		// Insert a brand-new (newest) event after the first page. Offset paging
		// would now re-show a row; keyset (seq < cursor) must not.
		if pages == 0 {
			mustDo(t, s.st.InsertEvent(&store.Event{CameraID: "cam1", StartTS: T + 100_000}))
		}
		cursor = rec.Header().Get("X-Next-Cursor")
		pages++
		if cursor == "" {
			break
		}
		if pages > 20 {
			t.Fatal("pagination did not terminate")
		}
	}

	// The 10 original events each appear exactly once; none skipped, none doubled.
	for seq := int64(1); seq <= 10; seq++ {
		if seen[seq] != 1 {
			t.Errorf("seq %d seen %d times, want exactly 1", seq, seen[seq])
		}
	}
	// The event inserted mid-pagination (seq 11) has a seq newer than page 1's
	// cursor, so it is correctly excluded from every subsequent keyset page.
	if seen[11] != 0 {
		t.Errorf("mid-insert seq 11 appeared %d times, want 0 (newer than the cursor)", seen[11])
	}
}

// TestEventsBadCursor: a malformed cursor is a client error, not a silent
// "reached the end".
func TestEventsBadCursor(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}
	// Valid base64url ("aGVsbG8" = "hello") but not an int64 key → client error.
	rec := s.reqAs("GET", "/api/v1/events?cursor=aGVsbG8", adminC)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor: got %d, want 400", rec.Code)
	}
	// Non-base64 payload is likewise rejected, not silently ignored.
	rec = s.reqAs("GET", "/api/v1/events?cursor=@@@", adminC)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-base64 cursor: got %d, want 400", rec.Code)
	}
}
