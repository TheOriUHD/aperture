package ha

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

// fakeClient records what the publisher sends instead of talking to a broker.
type fakeClient struct {
	mqtt.Client
	published map[string]published
}

type published struct {
	retained bool
	payload  any
}

func (f *fakeClient) IsConnected() bool { return true }
func (f *fakeClient) Publish(topic string, _ byte, retained bool, payload any) mqtt.Token {
	f.published[topic] = published{retained, payload}
	return nil
}

// fakeMessage is an inbound command.
type fakeMessage struct {
	mqtt.Message
	topic, payload string
}

func (m fakeMessage) Topic() string   { return m.topic }
func (m fakeMessage) Payload() []byte { return []byte(m.payload) }

func newTestPublisher(t *testing.T) (*Publisher, *store.Store, *fakeClient, *int) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	p := New(&config.Config{}, st, nil, func() string { return "Home" }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	fc := &fakeClient{published: map[string]published{}}
	p.client = fc
	reconciled := 0
	p.SetReconcile(func() { reconciled++ })
	return p, st, fc, &reconciled
}

func addCamera(t *testing.T, st *store.Store, id, mode string, withSchedule bool) {
	t.Helper()
	if err := st.CreateCamera(&store.Camera{ID: id, Name: id, RecordMode: mode, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if withSchedule {
		w := &store.Schedule{CameraID: id, Weekday: 1, StartMin: 420, EndMin: 1380, Mode: store.ModeContinuous}
		if err := st.SetSchedules(id, []*store.Schedule{w}); err != nil {
			t.Fatal(err)
		}
	}
}

// declaredRecordModes reads the Mode* constants out of models.go, so a mode added
// there but forgotten in store.RecordModes fails here rather than in HA's log.
func declaredRecordModes(t *testing.T) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "../store/models.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var modes []string
	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Values) != 1 || !strings.HasPrefix(spec.Names[0].Name, "Mode") {
			return true
		}
		if lit, ok := spec.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			v, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			modes = append(modes, v)
		}
		return true
	})
	if len(modes) == 0 {
		t.Fatal("found no Mode* constants in models.go; has the declaration moved?")
	}
	return modes
}

// TestDiscoveryOffersEveryRecordMode is the bug this file exists for: HA rejects
// any state outside the select's options, so a mode missing from discovery logs an
// error every 30 s and leaves the entity "unknown".
func TestDiscoveryOffersEveryRecordMode(t *testing.T) {
	p, st, fc, _ := newTestPublisher(t)
	addCamera(t, st, "cam1", store.ModeContinuous, false)
	cams, err := st.ListCameras()
	if err != nil {
		t.Fatal(err)
	}
	p.publishControlDiscovery("homeassistant", cams)

	msg, ok := fc.published["homeassistant/select/aperture_cam1_record_mode/config"]
	if !ok {
		t.Fatalf("no discovery published: %v", fc.published)
	}
	if !msg.retained {
		t.Error("discovery must be retained, or HA misses it after a restart")
	}
	var sel struct {
		UniqueID     string   `json:"unique_id"`
		CommandTopic string   `json:"command_topic"`
		StateTopic   string   `json:"state_topic"`
		Options      []string `json:"options"`
		Device       struct {
			Identifiers []string `json:"identifiers"`
		} `json:"device"`
	}
	if err := json.Unmarshal(msg.payload.([]byte), &sel); err != nil {
		t.Fatal(err)
	}
	for _, m := range declaredRecordModes(t) {
		if !slices.Contains(sel.Options, m) {
			t.Errorf("record mode %q is missing from the discovery options %v", m, sel.Options)
		}
	}
	// HA entity IDs hang off these; changing one duplicates entities in HA.
	if sel.UniqueID != "aperture_cam1_record_mode" ||
		sel.CommandTopic != "aperture/cam1/set/record_mode" ||
		sel.StateTopic != "aperture/cam1/record_mode" ||
		!slices.Equal(sel.Device.Identifiers, []string{"aperture_cam_cam1"}) {
		t.Errorf("an identity HA depends on changed: %+v", sel)
	}
}

func TestRecordModeCommands(t *testing.T) {
	p, st, fc, reconciled := newTestPublisher(t)
	addCamera(t, st, "cam1", store.ModeContinuous, true)
	send := func(payload string) {
		p.onCommand(nil, fakeMessage{topic: "aperture/cam1/set/record_mode", payload: payload})
	}
	mode := func() string {
		c, err := st.GetCamera("cam1")
		if err != nil {
			t.Fatal(err)
		}
		return c.RecordMode
	}

	// Into scheduled and back out: both directions apply, reach the recorder, and
	// report the new state on the retained state topic HA reads.
	for i, want := range []string{store.ModeScheduled, store.ModeContinuous} {
		delete(fc.published, "aperture/cam1/record_mode")
		send(strings.ToUpper(want)) // the case HA sends must not matter
		if got := mode(); got != want {
			t.Fatalf("mode = %q, want %q", got, want)
		}
		if *reconciled != i+1 {
			t.Errorf("recorder not told about %q: %d reconciles", want, *reconciled)
		}
		if s := fc.published["aperture/cam1/record_mode"]; s.payload != want || !s.retained {
			t.Errorf("state after %q: %+v", want, s)
		}
	}

	// Nonsense changes nothing.
	delete(fc.published, "aperture/cam1/record_mode")
	send("sometimes")
	if mode() != store.ModeContinuous || *reconciled != 2 {
		t.Errorf("an unknown mode was applied: %q, %d reconciles", mode(), *reconciled)
	}
	if _, ok := fc.published["aperture/cam1/record_mode"]; ok {
		t.Error("an unknown mode must not publish state")
	}
}

// TestScheduledRefusedWithoutASchedule pins the safety rule: the recorder runs only
// inside a window, so accepting this would silently stop the camera recording.
func TestScheduledRefusedWithoutASchedule(t *testing.T) {
	p, st, fc, reconciled := newTestPublisher(t)
	addCamera(t, st, "cam1", store.ModeContinuous, false)

	p.onCommand(nil, fakeMessage{topic: "aperture/cam1/set/record_mode", payload: "scheduled"})

	c, err := st.GetCamera("cam1")
	if err != nil {
		t.Fatal(err)
	}
	if c.RecordMode != store.ModeContinuous || *reconciled != 0 {
		t.Errorf("mode = %q after %d reconciles; the camera must keep recording", c.RecordMode, *reconciled)
	}
	if s, ok := fc.published["aperture/cam1/record_mode"]; ok {
		t.Errorf("a refused command must not report a state change: %+v", s)
	}
}

// TestPublishedStateIsAlwaysAnOption is the whole loop as HA sees it: for every
// mode a camera can be in, the state it reports is one the select accepts.
func TestPublishedStateIsAlwaysAnOption(t *testing.T) {
	for _, m := range store.RecordModes {
		p, st, fc, _ := newTestPublisher(t)
		addCamera(t, st, "cam1", m, false)
		cams, err := st.ListCameras()
		if err != nil {
			t.Fatal(err)
		}
		p.publishControlDiscovery("homeassistant", cams)
		p.publishControlState()

		var sel struct {
			Options []string `json:"options"`
		}
		if err := json.Unmarshal(fc.published["homeassistant/select/aperture_cam1_record_mode/config"].payload.([]byte), &sel); err != nil {
			t.Fatal(err)
		}
		state, _ := fc.published["aperture/cam1/record_mode"].payload.(string)
		if !slices.Contains(sel.Options, state) {
			t.Errorf("mode %q publishes state %q, outside the options %v", m, state, sel.Options)
		}
	}
}

// HA's Recording sensor said ON all night for a camera outside its schedule. The
// topic is retained, so the first pass has to publish even an unchanged-looking OFF:
// what the broker holds may be an ON from before this process started.
func TestRecordingSensorFollowsTheSchedule(t *testing.T) {
	p, st, fc, _ := newTestPublisher(t)
	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", Enabled: true, RecordMode: store.ModeScheduled, RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	inWindow := false
	p.SetShouldRecord(func(*store.Camera) bool { return inWindow })
	const topic = stateTopicBase + "/c1/recording"

	p.publishMotionRecording()
	if got := fc.published[topic]; got.payload != "OFF" || !got.retained {
		t.Fatalf("first pass published %+v, want a retained OFF", got)
	}

	delete(fc.published, topic)
	p.publishMotionRecording()
	if _, sent := fc.published[topic]; sent {
		t.Error("republished a state that hadn't changed")
	}

	inWindow = true
	p.publishMotionRecording()
	if got := fc.published[topic]; got.payload != "ON" {
		t.Errorf("window opened: published %+v, want ON", got)
	}
}

// A deleted camera stayed in Home Assistant as a dead device: its topics are
// retained, and nothing cleared them. Publishing everything a camera has and then
// forgetting it must leave none of its topics holding a value.
func TestForgottenCameraLeavesNothingRetained(t *testing.T) {
	p, st, fc, _ := newTestPublisher(t)
	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", Enabled: true, RecordMode: store.ModeContinuous, RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	p.publishDiscovery()
	p.publishMotionRecording()
	p.publishControlState()
	p.pub(stateTopicBase+"/c1/snapshot", true, []byte("jpeg"))

	listed := cameraTopics(p.prefix(), "c1")
	for topic := range fc.published {
		if strings.Contains(topic, "c1") && !slices.Contains(listed, topic) {
			t.Errorf("published %s, which deleting the camera wouldn't clear", topic)
		}
	}

	p.ForgetCamera("c1")

	for _, topic := range listed {
		if got := fc.published[topic]; got.payload != "" || !got.retained {
			t.Errorf("%s holds %+v after the camera was forgotten", topic, got)
		}
	}
	if _, kept := p.recording["c1"]; kept {
		t.Error("kept publish state for a camera that no longer exists")
	}
}
