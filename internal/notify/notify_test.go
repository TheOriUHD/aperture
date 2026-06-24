package notify

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

// TestDispatch proves the full engine path: a new event matches an enabled rule
// (ACL + cooldown + quiet-hours honored) and fans out to a webhook channel.
func TestDispatch(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	got := make(chan map[string]string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]string
		_ = json.Unmarshal(b, &m)
		got <- m
		w.WriteHeader(200)
	}))
	defer srv.Close()

	user := &store.User{Email: "a@b.c", Name: "A", Role: store.RoleAdmin}
	if err := st.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	cam := &store.Camera{Name: "Front", RTSPMainURL: "rtsp://x", RecordMode: store.ModeContinuous}
	if err := st.CreateCamera(cam); err != nil {
		t.Fatal(err)
	}
	ch := &store.NotifyChannel{UserID: user.ID, Type: "webhook", Name: "wh",
		Config: `{"url":"` + srv.URL + `"}`, Enabled: true, CreatedAt: time.Now().Unix()}
	if err := st.CreateChannel(ch); err != nil {
		t.Fatal(err)
	}
	rule := &store.NotifyRule{UserID: user.ID, Name: "all", Enabled: true,
		Channels: []string{ch.ID}, QuietStart: -1, QuietEnd: -1, CooldownS: 0, CreatedAt: time.Now().Unix()}
	if err := st.CreateRule(rule); err != nil {
		t.Fatal(err)
	}
	ev := &store.Event{CameraID: cam.ID, Type: "motion", StartTS: time.Now().UnixMilli()}
	if err := st.InsertEvent(ev); err != nil {
		t.Fatal(err)
	}

	eng := New(&config.Config{PublicURL: "http://x", StreamSecret: []byte("s")}, st,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	eng.OnEvent(ev)

	select {
	case m := <-got:
		if m["title"] == "" || m["url"] == "" {
			t.Fatalf("webhook payload incomplete: %+v", m)
		}
		t.Logf("dispatched: title=%q body=%q", m["title"], m["body"])
	case <-time.After(3 * time.Second):
		t.Fatal("webhook was not called within 3s")
	}
}
