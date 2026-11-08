package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/TheOriUHD/aperture/internal/ha"
	"github.com/TheOriUHD/aperture/internal/record"
	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
	"github.com/TheOriUHD/aperture/internal/weather"
)

// A dashboard counted 4 cameras on a system running 2, because it drew the list the
// Settings page uses to switch cameras back on. Real tokens through the real
// routes: what decides this is the credential class the auth layer assigns.
func TestOnlyEnabledCamerasReachOutsideIntegrations(t *testing.T) {
	s := newTestServerNoRouter(t)
	go2rtc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(go2rtc.Close)
	sm, err := stream.NewManager(go2rtc.URL, "rtsp://127.0.0.1:1")
	mustDo(t, err)
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	s.sm, s.ha, s.weather = sm, ha.New(s.cfg, s.st, nil, func() string { return "" }, lg), weather.New(s.st, lg)
	s.router = s.routes()

	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "on", Name: "On", Enabled: true}))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "off", Name: "Off"}))

	scopes := []string{ScopeCamerasRead, ScopeSystemRead, ScopeStorageRead}
	_, integration, err := s.st.CreateAPIToken(admin.ID, "dashboard", 0, scopes, nil)
	mustDo(t, err)
	extension, err := s.st.CreateExtensionToken(admin.ID, "com.example.tapo-control", "Tapo Control", scopes, nil)
	mustDo(t, err)

	cameraIDs := func(t *testing.T, path string, rec *httptest.ResponseRecorder) []string {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body)
		}
		var rows []struct {
			ID       string `json:"id"`
			CameraID string `json:"cameraId"`
		}
		if path == "/api/v1/cameras" {
			mustDo(t, json.Unmarshal(rec.Body.Bytes(), &rows))
		} else {
			var body struct {
				Cameras json.RawMessage `json:"cameras"`
			}
			mustDo(t, json.Unmarshal(rec.Body.Bytes(), &body))
			mustDo(t, json.Unmarshal(body.Cameras, &rows))
		}
		ids := []string{}
		for _, r := range rows {
			ids = append(ids, r.ID+r.CameraID)
		}
		slices.Sort(ids)
		return ids
	}

	onlyOn, both := []string{"on"}, []string{"off", "on"}
	for _, c := range []struct {
		name  string
		who   cred
		query string
		want  []string
	}{
		{"an integration sees what runs", cred{"dashboard", bearerCred(integration)}, "", onlyOn},
		{"unless it asks for the rest", cred{"dashboard", bearerCred(integration)}, "?includeDisabled=true", both},
		{"and only exactly that asks", cred{"dashboard", bearerCred(integration)}, "?includeDisabled=false", onlyOn},
		{"an extension can be granted a disabled camera", cred{"ext", bearerCred(extension)}, "", both},
		{"the web app switches them back on", cred{"admin", cookieCred(sessionCookieFor(t, s, admin))}, "", both},
		{"so does nvrctl", cred{"cli", bearerCred(testCLIToken)}, "", both},
	} {
		for _, path := range []string{"/api/v1/cameras", "/api/v1/system/status", "/api/v1/system/storage"} {
			t.Run(c.name+" "+path, func(t *testing.T) {
				got := cameraIDs(t, path, s.reqAs("GET", path+c.query, c.who))
				if !slices.Equal(got, c.want) {
					t.Errorf("got %v, want %v", got, c.want)
				}
			})
		}
	}
}

// newStatusServer is a test server that can answer /system/status for real,
// with a real recorder deciding what each camera's schedule means right now.
func newStatusServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServerNoRouter(t)
	go2rtc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(go2rtc.Close)
	sm, err := stream.NewManager(go2rtc.URL, "rtsp://127.0.0.1:1")
	mustDo(t, err)
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	s.sm, s.ha, s.weather = sm, ha.New(s.cfg, s.st, nil, func() string { return "" }, lg), weather.New(s.st, lg)
	s.rec = record.New(s.cfg, s.st, sm, nil, lg)
	s.router = s.routes()
	return s
}
