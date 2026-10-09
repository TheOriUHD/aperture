package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// fakeInterop stands in for the real ONVIF/HomeKit supervisors.
type fakeInterop struct {
	running  bool
	failWith error
	calls    int
}

func (f *fakeInterop) SetEnabled(on bool) error {
	f.calls++
	if f.failWith != nil {
		return f.failWith
	}
	f.running = on
	return nil
}
func (f *fakeInterop) Running() bool        { return f.running }
func (f *fakeInterop) Info() map[string]any { return map[string]any{"pin": "12345678"} }

// TestInteropToggle pins the ONVIF/HomeKit UI contract: toggling starts or stops
// the subsystem AND persists the choice, the reported `running` is the TRUE state
// from the service (not the request), and a service that fails to start is a 500
// with the setting left untouched — never a false green light.
func TestInteropToggle(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: store.RoleAdmin}
	mustDo(t, s.st.CreateUser(admin))
	sess := cred{name: "admin", apply: cookieCred(sessionCookieFor(t, s, admin))}
	onvif, hk := &fakeInterop{}, &fakeInterop{}
	s.SetInterop(onvif, hk)

	// Off to begin with.
	rec := s.reqAs("GET", "/api/v1/system/interop", sess)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"running":false`) {
		t.Fatalf("initial GET: %d %s", rec.Code, rec.Body.String())
	}

	// Enable ONVIF: service started, setting persisted, running reported true.
	rec = s.reqBody("PUT", "/api/v1/system/interop", sess, strings.NewReader(`{"onvif":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("enable onvif: %d %s", rec.Code, rec.Body.String())
	}
	if !onvif.running {
		t.Error("the ONVIF service was never started")
	}
	if !s.st.GetSettingBool(settingONVIFEnabled) {
		t.Error("the choice was not persisted")
	}
	if hk.calls != 0 {
		t.Error("an absent field must leave the other service alone")
	}

	// Disable again.
	rec = s.reqBody("PUT", "/api/v1/system/interop", sess, strings.NewReader(`{"onvif":false}`))
	if rec.Code != http.StatusOK || onvif.running {
		t.Errorf("disable onvif: %d, running=%v", rec.Code, onvif.running)
	}
	if s.st.GetSettingBool(settingONVIFEnabled) {
		t.Error("disable must persist too")
	}

	// A service that cannot start is an error, and the setting stays off so the
	// UI never claims it is on.
	hk.failWith = errors.New("port in use")
	rec = s.reqBody("PUT", "/api/v1/system/interop", sess, strings.NewReader(`{"homekit":true}`))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("failed start: want 500, got %d %s", rec.Code, rec.Body.String())
	}
	if s.st.GetSettingBool(settingHomeKitEnabled) {
		t.Error("a failed start must NOT be recorded as enabled")
	}
}

// TestInteropUnavailable pins that a nil service reports unavailable rather than
// pretending it can be switched on.
func TestInteropUnavailable(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: store.RoleAdmin}
	mustDo(t, s.st.CreateUser(admin))
	sess := cred{name: "admin", apply: cookieCred(sessionCookieFor(t, s, admin))}

	rec := s.reqAs("GET", "/api/v1/system/interop", sess)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"available":false`) {
		t.Fatalf("want available:false with no services wired, got %s", rec.Body.String())
	}
}
