package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/TheOriUHD/aperture/internal/secret"
	"github.com/TheOriUHD/aperture/internal/store"
	"github.com/TheOriUHD/aperture/internal/stream"
)

// Camera logins never leave the server; they are only ever replaced. These pin
// the API side of that: what each caller is shown, and how a save that carries
// the masked login back is resolved (see internal/urlcred for the rules).

const (
	credMain = "rtsp://admin:hunter2secret@10.0.0.5:554/Preview_01_main"
	credSub  = "rtsp://admin:hunter2secret@10.0.0.5:554/Preview_01_sub#video=copy#audio=copy"
)

func newCredTest(t *testing.T) (s *Server, admin, viewer cred) {
	t.Helper()
	s = newTestServer(t)
	a := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: store.RoleAdmin}
	v := &store.User{ID: "v1", Email: "v@t", Name: "V", Role: store.RoleViewer}
	mustDo(t, s.st.CreateUser(a))
	mustDo(t, s.st.CreateUser(v))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "Schlafzimmer", RTSPMainURL: credMain,
		RTSPSubURL: credSub, Enabled: true, RecordMode: store.ModeContinuous, RetentionDays: 7}))
	mustDo(t, s.st.SetUserCameras(v.ID, []string{"cam1"}))
	return s, cred{"admin", cookieCred(sessionCookieFor(t, s.am, a))}, cred{"viewer", cookieCred(sessionCookieFor(t, s.am, v))}
}

func TestCameraLinksNeverLeaveWithTheirLogin(t *testing.T) {
	s, admin, viewer := newCredTest(t)
	for _, path := range []string{"/api/v1/cameras", "/api/v1/cameras/cam1"} {
		rec := s.reqAs("GET", path, admin)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || strings.Contains(body, "hunter2secret") || strings.Contains(body, "admin:") {
			t.Fatalf("admin %s leaked the login: %d %s", path, rec.Code, body)
		}
		if !strings.Contains(body, "rtsp://•••••:•••••@10.0.0.5:554/Preview_01_main") ||
			!strings.Contains(body, "rtsp://•••••:•••••@10.0.0.5:554/Preview_01_sub#video=copy#audio=copy") {
			t.Errorf("admin %s should see both links with the login masked: %s", path, body)
		}

		// A viewer has no use for a camera's address and gets no link at all.
		rec = s.reqAs("GET", path, viewer)
		body = rec.Body.String()
		if rec.Code != http.StatusOK || strings.Contains(body, "10.0.0.5") || strings.Contains(body, "hunter2secret") {
			t.Errorf("viewer %s must get no link: %d %s", path, rec.Code, body)
		}
	}

	// The ETag is sent to every caller; it must not be a hash over the password.
	one := &store.Camera{ID: "x", RTSPMainURL: "rtsp://u:one@h/x"}
	two := &store.Camera{ID: "x", RTSPMainURL: "rtsp://u:two@h/x"}
	if etagOf(cameraView(one, true)) != etagOf(cameraView(two, true)) {
		t.Error("the camera ETag depends on the password")
	}
}

func TestCameraSaveKeepsTheMaskedLogin(t *testing.T) {
	s, admin, _ := newCredTest(t)
	stored := func() *store.Camera {
		c, err := s.st.GetCamera("cam1")
		mustDo(t, err)
		return c
	}
	put := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		return s.reqBody("PUT", "/api/v1/cameras/cam1", admin, strings.NewReader(body))
	}

	// Other settings only: the links are not sent and stay exactly as saved.
	if rec := put(`{"name":"Renamed","recordMode":"motion"}`); rec.Code != http.StatusOK {
		t.Fatalf("settings-only save: %d %s", rec.Code, rec.Body.String())
	}
	if c := stored(); c.RTSPMainURL != credMain || c.RTSPSubURL != credSub {
		t.Fatalf("a settings-only save changed the links: %q %q", c.RTSPMainURL, c.RTSPSubURL)
	}

	// The links sent back exactly as they were shown (masked): still unchanged.
	var shown map[string]any
	mustDo(t, json.Unmarshal(s.reqAs("GET", "/api/v1/cameras/cam1", admin).Body.Bytes(), &shown))
	body, _ := json.Marshal(map[string]any{"rtspMainUrl": shown["rtspMainUrl"], "rtspSubUrl": shown["rtspSubUrl"], "retentionDays": 9})
	if rec := put(string(body)); rec.Code != http.StatusOK {
		t.Fatalf("masked round trip: %d %s", rec.Code, rec.Body.String())
	}
	if c := stored(); c.RTSPMainURL != credMain || c.RTSPSubURL != credSub || c.RetentionDays != 9 {
		t.Fatalf("the masked round trip changed the links: %q %q (retention %d)", c.RTSPMainURL, c.RTSPSubURL, c.RetentionDays)
	}

	// A new address with the dots left in place is refused, and nothing is saved.
	rec := put(`{"rtspMainUrl":"rtsp://•••••:•••••@10.6.6.6:554/Preview_01_main"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "address changed") {
		t.Fatalf("new address with the saved login: %d %s", rec.Code, rec.Body.String())
	}
	if c := stored(); c.RTSPMainURL != credMain {
		t.Fatalf("a refused save changed the link: %q", c.RTSPMainURL)
	}

	// A new password typed over the dots is used; the sub link shared the old
	// login, so it follows (keeping its go2rtc options); the reply stays masked.
	rec = put(`{"rtspMainUrl":"rtsp://•••••:newpass@10.0.0.5:554/Preview_01_main"}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "newpass") {
		t.Fatalf("password change: %d %s", rec.Code, rec.Body.String())
	}
	c := stored()
	if c.RTSPMainURL != "rtsp://admin:newpass@10.0.0.5:554/Preview_01_main" {
		t.Errorf("main after the password change = %q", c.RTSPMainURL)
	}
	if c.RTSPSubURL != "rtsp://admin:newpass@10.0.0.5:554/Preview_01_sub#video=copy#audio=copy" {
		t.Errorf("sub did not follow the shared login: %q", c.RTSPSubURL)
	}

	// Clearing the sub link still works.
	if rec := put(`{"rtspSubUrl":""}`); rec.Code != http.StatusOK || stored().RTSPSubURL != "" {
		t.Errorf("clearing the sub link: %d, sub=%q", rec.Code, stored().RTSPSubURL)
	}
}

func TestCameraCreateRejectsAMaskedLink(t *testing.T) {
	s, admin, _ := newCredTest(t)
	rec := s.reqBody("POST", "/api/v1/cameras", admin,
		strings.NewReader(`{"name":"Copy","rtspMainUrl":"rtsp://•••••:•••••@10.0.0.5:554/Preview_01_main"}`))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "no saved login") {
		t.Fatalf("masked link on a new camera: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCameraConnectionTestFillsTheSavedLogin(t *testing.T) {
	s, admin, _ := newCredTest(t)
	var mu sync.Mutex
	var registered []string
	go2rtc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/streams":
			mu.Lock()
			registered = append(registered, r.URL.Query().Get("src"))
			mu.Unlock()
		case r.URL.Path == "/api/frame.jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("jpeg"))
		}
	}))
	defer go2rtc.Close()
	sm, err := stream.NewManager(go2rtc.URL, "rtsp://127.0.0.1:8554")
	mustDo(t, err)
	s.sm = sm

	rec := s.reqBody("POST", "/api/v1/cameras/test", admin,
		strings.NewReader(`{"cameraId":"cam1","rtspMainUrl":"rtsp://•••••:•••••@10.0.0.5:554/Preview_01_main"}`))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) || strings.Contains(rec.Body.String(), "hunter2secret") {
		t.Fatalf("masked test: %d %s", rec.Code, rec.Body.String())
	}
	mu.Lock()
	got := append([]string(nil), registered...)
	mu.Unlock()
	if len(got) != 1 || got[0] != credMain {
		t.Fatalf("go2rtc was asked to test %q, want the saved link", got)
	}

	// The same rules as a save: no refill for a new address or without a camera.
	for _, body := range []string{
		`{"cameraId":"cam1","rtspMainUrl":"rtsp://•••••:•••••@10.6.6.6/x"}`,
		`{"rtspMainUrl":"rtsp://•••••:•••••@10.0.0.5/x"}`,
	} {
		if rec := s.reqBody("POST", "/api/v1/cameras/test", admin, strings.NewReader(body)); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", body, rec.Code)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(registered) != 1 {
		t.Errorf("a refused test still reached go2rtc: %q", registered)
	}
}

func TestCameraAccountIsWriteOnly(t *testing.T) {
	s, admin, _ := newCredTest(t)
	c, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	mustDo(t, err)
	s.cipher = c

	rec := s.reqBody("PUT", "/api/v1/cameras/cam1/onvif", admin, strings.NewReader(`{"user":"spotuser","password":"spotpass"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("store the account: %d %s", rec.Code, rec.Body.String())
	}
	for _, r := range []*httptest.ResponseRecorder{rec, s.reqAs("GET", "/api/v1/cameras/cam1/onvif", admin)} {
		body := r.Body.String()
		if strings.Contains(body, "spotuser") || strings.Contains(body, "spotpass") || strings.Contains(body, "hunter2secret") {
			t.Fatalf("the camera account leaked: %s", body)
		}
		if !strings.Contains(body, `"hasUser":true`) || !strings.Contains(body, `"hasPassword":true`) {
			t.Errorf("the stored account should be reported as present: %s", body)
		}
	}
	cfg, err := s.st.CameraONVIFConfig("cam1")
	mustDo(t, err)
	if cfg.User != "spotuser" {
		t.Errorf("stored user = %q", cfg.User)
	}
}
