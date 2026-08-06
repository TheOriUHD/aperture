package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// TestCollectionETagRoundTrip covers Item 2: a token can list a collection,
// read a real per-item ETag, and use it as If-Match — getting 200 on a match,
// 412 on a stale value, and 428 when omitted. This is the lost-update protection
// that If-Match: * alone (existence only) could not provide.
func TestCollectionETagRoundTrip(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	rule := &store.NotifyRule{UserID: admin.ID, Name: "R", Enabled: true, QuietStart: -1, QuietEnd: -1}
	mustDo(t, s.st.CreateRule(rule))

	_, tok, err := s.st.CreateAPIToken(admin.ID, "ext", 0, []string{ScopeAutomationRead, ScopeAutomationWrite}, nil)
	mustDo(t, err)
	tokenC := cred{"token", bearerCred(tok)}

	// List exposes a real per-item ETag (not "*").
	rec := s.reqAs("GET", "/api/v1/notify/rules", tokenC)
	if rec.Code != http.StatusOK {
		t.Fatalf("list rules: got %d, want 200", rec.Code)
	}
	var list []map[string]any
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &list))
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	etag, _ := list[0]["etag"].(string)
	if etag == "" || etag == "*" {
		t.Fatalf("list item etag = %q, want a real strong ETag", etag)
	}

	put := func(ifMatch string) *httptest.ResponseRecorder {
		body := `{"name":"Renamed","enabled":true,"cameras":[],"eventTypes":[],"channels":[]}`
		r := httptest.NewRequest("PUT", "/api/v1/notify/rules/"+rule.ID, strings.NewReader(body))
		tokenC.apply(r)
		if ifMatch != "" {
			r.Header.Set("If-Match", ifMatch)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}

	// Missing If-Match → 428 (required for token writes).
	if rec := put(""); rec.Code != http.StatusPreconditionRequired {
		t.Errorf("no If-Match: got %d, want 428", rec.Code)
	}
	// Stale If-Match → 412 (real staleness protection, not just existence).
	if rec := put(`"stale000000000000000000000000dead"`); rec.Code != http.StatusPreconditionFailed {
		t.Errorf("stale If-Match: got %d, want 412", rec.Code)
	}
	// The real list ETag → 200.
	if rec := put(etag); rec.Code != http.StatusOK {
		t.Errorf("matching If-Match from the list: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}
