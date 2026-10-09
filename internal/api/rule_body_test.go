package api

import (
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// bodies the client sends. The only difference is the presence of "id".
const ruleBodyWithID = `{"id":"%ID%","name":"Renamed","enabled":true,"cameras":["c1"],"eventTypes":[],"channels":["ch1"],"quietStart":-1,"quietEnd":-1,"cooldownSec":120,"conditions":{"timeOfDay":"","onlyPrecip":false,"objects":["person"]}}`
const ruleBodyNoID = `{"name":"Renamed","enabled":true,"cameras":["c1"],"eventTypes":[],"channels":["ch1"],"quietStart":-1,"quietEnd":-1,"cooldownSec":120,"conditions":{"timeOfDay":"","onlyPrecip":false,"objects":["person"]}}`

func newRuleTest(t *testing.T) (*Server, cred, *store.NotifyRule) {
	t.Helper()
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	rule := &store.NotifyRule{UserID: admin.ID, Name: "R", Enabled: true, QuietStart: -1, QuietEnd: -1}
	mustDo(t, s.st.CreateRule(rule))
	return s, cred{"admin", cookieCred(sessionCookieFor(t, s, admin))}, rule
}

// TestRuleUpdateRejectsIdInBody pins the bug: the id belongs in the URL, not the
// body — DisallowUnknownFields rejects it, which surfaces as "invalid request
// body" in the UI. This is what every rule edit was hitting.
func TestRuleUpdateRejectsIdInBody(t *testing.T) {
	s, adminC, rule := newRuleTest(t)
	body := strings.ReplaceAll(ruleBodyWithID, "%ID%", rule.ID)
	rec := s.reqBody("PUT", "/api/v1/notify/rules/"+rule.ID, adminC, strings.NewReader(body))
	if rec.Code != 400 {
		t.Fatalf("body carrying id: got %d, want 400 (the pre-fix bug)", rec.Code)
	}
}

// TestRuleObjectOnlyRoundTrip is the required test: the correct body (id only in
// the URL) with an object-only condition round-trips through the real handler and
// the object condition is persisted.
func TestRuleObjectOnlyRoundTrip(t *testing.T) {
	s, adminC, rule := newRuleTest(t)
	rec := s.reqBody("PUT", "/api/v1/notify/rules/"+rule.ID, adminC, strings.NewReader(ruleBodyNoID))
	if rec.Code != 200 {
		t.Fatalf("correct body (no id) with objects: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	got, err := s.st.GetRule(rule.ID)
	mustDo(t, err)
	if len(got.Conditions.Objects) != 1 || got.Conditions.Objects[0] != "person" {
		t.Fatalf("object condition not persisted: %+v", got.Conditions)
	}
}
