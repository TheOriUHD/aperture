package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/notify"
	"github.com/TheOriUHD/aperture/internal/store"
)

// TestAutomationScopes covers 3.4 scopes on the notify/automation routes.
func TestAutomationScopes(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mk := func(scopes []string) cred {
		_, tok, err := s.st.CreateAPIToken(admin.ID, "t", 0, scopes, nil)
		mustDo(t, err)
		return cred{"token", bearerCred(tok)}
	}
	// automation:read reads rules; no scope is denied; automation:write is needed
	// to reach the admin webhooks list (owner is admin).
	if rec := s.reqAs("GET", "/api/v1/notify/rules", mk([]string{ScopeAutomationRead})); rec.Code != http.StatusOK {
		t.Errorf("automation:read GET rules: got %d, want 200", rec.Code)
	}
	if rec := s.reqAs("GET", "/api/v1/notify/rules", mk(nil)); rec.Code != http.StatusForbidden {
		t.Errorf("no-scope GET rules: got %d, want 403", rec.Code)
	}
	if rec := s.reqAs("GET", "/api/v1/webhooks", mk([]string{ScopeAutomationRead})); rec.Code != http.StatusOK {
		t.Errorf("automation:read GET webhooks: got %d, want 200", rec.Code)
	}
}

// TestAlarmEndpoint covers GET/PUT /alarm and the alarm.state emitter.
func TestAlarmEndpoint(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	// Default (unset) is armed.
	rec := s.reqAs("GET", "/api/v1/alarm", adminC)
	var st struct{ Armed bool `json:"armed"` }
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &st))
	if !st.Armed {
		t.Errorf("default alarm should be armed")
	}

	// Disarm emits alarm.state.
	_, ch := s.notify.SubscribeFeed(notify.FeedFilter{Types: map[string]bool{"alarm.state": true}}, nil)
	if rec := s.reqBody("PUT", "/api/v1/alarm", adminC, strings.NewReader(`{"armed":false}`)); rec.Code != http.StatusOK {
		t.Fatalf("PUT /alarm: got %d, want 200", rec.Code)
	}
	select {
	case f := <-ch:
		if !strings.Contains(string(f.Data), "alarm.state") {
			t.Errorf("frame = %s, want alarm.state", f.Data)
		}
	default:
		t.Fatal("disarm did not emit alarm.state")
	}
	if v, _ := s.st.GetSetting("armed"); v != "false" {
		t.Errorf("armed setting = %q, want false", v)
	}

	// A token needs alarm:arm.
	_, tok, err := s.st.CreateAPIToken(admin.ID, "t", 0, nil, nil)
	mustDo(t, err)
	if rec := s.reqAs("GET", "/api/v1/alarm", cred{"token", bearerCred(tok)}); rec.Code != http.StatusForbidden {
		t.Errorf("no-scope token GET /alarm: got %d, want 403", rec.Code)
	}
}

// TestRuleObjectConditionsRoundTrip covers the 3.4 marshal fix: an object-only
// rule condition survives save/reload (previously silently dropped).
func TestRuleObjectConditionsRoundTrip(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/r.db")
	mustDo(t, err)
	defer st.Close()

	rule := &store.NotifyRule{
		UserID: "u1", Name: "person-only", Enabled: true,
		Conditions: store.RuleConditions{Objects: []string{"person"}}, // no timeOfDay/precip
	}
	mustDo(t, st.CreateRule(rule))
	got, err := st.GetRule(rule.ID)
	mustDo(t, err)
	if len(got.Conditions.Objects) != 1 || got.Conditions.Objects[0] != "person" {
		t.Fatalf("object condition lost on round-trip: %+v", got.Conditions)
	}
}
