package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// These pin the audit finding behind the id-in-body class of bug: Item 2 added a
// per-item "etag" to the automations and webhooks LIST responses (for If-Match
// round-trips), and the web edits those by spreading a fetched list object
// ({ ...item, enabled }) straight into the PUT body. That echoes the server-emitted
// "etag" field, which neither store struct declares, so decodeJSON's
// DisallowUnknownFields rejects it with 400 "invalid request body" — every
// automation save/toggle and every webhook toggle broke the moment Item 2 shipped.
// The client now strips the envelope tag at the api.ts write boundary; these assert
// the server contract both ways so the reason for that strip cannot silently drift.

func TestAutomationUpdateRejectsEnvelopeEtag(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	auto := &store.Automation{
		Name: "A", Enabled: true,
		Trigger:    json.RawMessage("{}"),
		Conditions: json.RawMessage("{}"),
		Actions:    json.RawMessage("[]"),
	}
	mustDo(t, s.st.CreateAutomation(auto))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	put := func(body string) *httptest.ResponseRecorder {
		return s.reqBody("PUT", "/api/v1/automations/"+auto.ID, adminC, strings.NewReader(body))
	}
	// The buggy body: a spread list item still carrying the server-emitted etag.
	withEtag := `{"id":"` + auto.ID + `","name":"Renamed","enabled":false,"trigger":{},"conditions":{},"actions":[],"cooldownMs":0,"etag":"\"deadbeef\""}`
	if rec := put(withEtag); rec.Code != http.StatusBadRequest {
		t.Fatalf("automation body carrying envelope etag: got %d, want 400", rec.Code)
	} else if b := rec.Body.String(); !strings.Contains(b, "unknown field") || !strings.Contains(b, "etag") {
		t.Errorf("400 message should name the offending field, got: %s", b)
	}
	// The fixed body: envelope stripped, everything the server declares intact.
	noEtag := `{"id":"` + auto.ID + `","name":"Renamed","enabled":false,"trigger":{},"conditions":{},"actions":[],"cooldownMs":0}`
	if rec := put(noEtag); rec.Code != http.StatusOK {
		t.Fatalf("automation body without etag: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

func TestWebhookUpdateRejectsEnvelopeEtag(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	wh := &store.Webhook{Name: "H", URL: "http://x", Events: []string{}, Enabled: true}
	mustDo(t, s.st.CreateWebhook(wh))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	put := func(body string) *httptest.ResponseRecorder {
		return s.reqBody("PUT", "/api/v1/webhooks/"+wh.ID, adminC, strings.NewReader(body))
	}
	withEtag := `{"id":"` + wh.ID + `","name":"H","url":"http://x","events":[],"enabled":false,"createdAt":0,"etag":"\"deadbeef\""}`
	if rec := put(withEtag); rec.Code != http.StatusBadRequest {
		t.Fatalf("webhook body carrying envelope etag: got %d, want 400", rec.Code)
	} else if b := rec.Body.String(); !strings.Contains(b, "unknown field") || !strings.Contains(b, "etag") {
		t.Errorf("400 message should name the offending field, got: %s", b)
	}
	noEtag := `{"id":"` + wh.ID + `","name":"H","url":"http://x","events":[],"enabled":false,"createdAt":0}`
	if rec := put(noEtag); rec.Code != http.StatusOK {
		t.Fatalf("webhook body without etag: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}
