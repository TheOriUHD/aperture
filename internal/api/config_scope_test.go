package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/notify"
	"github.com/TheOriUHD/aperture/internal/store"
)

// TestConfigScopeAndK3 covers the 3.3 config scopes and the K3 resolution:
// tokens need the per-domain scope, but a viewer session still reads the
// display-config settings blob (the TopBar depends on it).
func TestConfigScopeAndK3(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	viewer := &store.User{ID: "v1", Email: "v@t", Name: "V", Role: "viewer"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateUser(viewer))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))

	mk := func(scopes, grants []string) cred {
		_, tok, err := s.st.CreateAPIToken(admin.ID, "t", 0, scopes, grants)
		mustDo(t, err)
		return cred{"token", bearerCred(tok)}
	}
	check := func(c cred, path string, want int) {
		if rec := s.reqAs("GET", path, c); rec.Code != want {
			t.Errorf("%s → %d, want %d", path, rec.Code, want)
		}
	}

	// Per-domain read scopes gate their routes; a mismatched scope is denied.
	check(mk([]string{ScopeStorageRead}, nil), "/api/v1/system/storage", http.StatusOK)
	check(mk([]string{ScopeSiteRead}, nil), "/api/v1/system/storage", http.StatusForbidden)
	check(mk([]string{ScopeSiteRead}, nil), "/api/v1/system/settings", http.StatusOK)
	check(mk(nil, nil), "/api/v1/system/settings", http.StatusForbidden)
	check(mk([]string{ScopeSchedulesRead}, []string{"cam1"}), "/api/v1/cameras/cam1/schedule", http.StatusOK)
	check(mk([]string{ScopeZonesRead}, []string{"cam1"}), "/api/v1/cameras/cam1/schedule", http.StatusForbidden)

	// K3: a viewer SESSION (not a token) still reads the settings blob — scope
	// checks are bypassed for sessions, so the TopBar isn't broken for viewers.
	viewerC := cred{"viewer", cookieCred(sessionCookieFor(t, s.am, viewer))}
	if rec := s.reqAs("GET", "/api/v1/system/settings", viewerC); rec.Code != http.StatusOK {
		t.Fatalf("viewer session GET /system/settings: got %d, want 200 (K3 must not break the header)", rec.Code)
	}
}

// TestConfigUpdatedEmit covers the config.updated emitter: a config mutation
// publishes a config.updated change-feed message.
func TestConfigUpdatedEmit(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	adminC := cred{"admin", cookieCred(sessionCookieFor(t, s.am, admin))}

	_, ch := s.notify.SubscribeFeed(notify.FeedFilter{Types: map[string]bool{"config.updated": true}}, nil)
	if rec := s.reqBody("PUT", "/api/v1/system/settings", adminC, strings.NewReader(`{"siteName":"Test Site"}`)); rec.Code != http.StatusOK {
		t.Fatalf("PUT settings: got %d, want 200", rec.Code)
	}
	select {
	case f := <-ch:
		if string(f.Data) == "" || !strings.Contains(string(f.Data), "config.updated") {
			t.Errorf("frame = %s, want a config.updated message", f.Data)
		}
	default:
		t.Fatal("config mutation did not emit config.updated")
	}
}
