package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/store"
)

// A camera idle by design and a broken one both have no new footage. Everything
// reading /system/status has to be able to tell them apart: Aperture's health dot and
// a dashboard only go red, and only alert, when a camera should be recording and isn't.
func TestStatusTellsIdleFromBroken(t *testing.T) {
	s := newStatusServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))

	now := time.Now()
	allWeek := []*store.Schedule{}
	for d := 0; d < 7; d++ {
		allWeek = append(allWeek, &store.Schedule{Weekday: d, StartMin: 0, EndMin: 24 * 60, Mode: store.ModeContinuous})
	}
	notToday := []*store.Schedule{{Weekday: (int(now.Weekday()) + 3) % 7, StartMin: 0, EndMin: 24 * 60, Mode: store.ModeContinuous}}

	old, fresh := now.Add(-8*time.Hour).UnixMilli(), now.Add(-5*time.Second).UnixMilli()
	for _, c := range []struct {
		id      string
		enabled bool
		mode    string
		sched   []*store.Schedule
		newest  int64
	}{
		{"idle", true, store.ModeScheduled, notToday, old},
		{"broken", true, store.ModeScheduled, allWeek, old},
		{"working", true, store.ModeContinuous, nil, fresh},
		{"never", true, store.ModeNever, nil, old},
		{"disabled", false, store.ModeContinuous, nil, old},
	} {
		mustDo(t, s.st.CreateCamera(&store.Camera{ID: c.id, Name: c.id, RTSPMainURL: "rtsp://cam/1",
			Enabled: c.enabled, RecordMode: c.mode}))
		if c.sched != nil {
			mustDo(t, s.st.SetSchedules(c.id, c.sched))
		}
		mustDo(t, s.st.InsertSegment(&store.Segment{CameraID: c.id, Path: c.id + ".ts",
			StartTS: c.newest - 10_000, EndTS: c.newest, DurationMS: 10_000, SizeBytes: 1}))
	}
	admC := cred{"admin", cookieCred(sessionCookieFor(t, s, admin))}

	rec := s.reqAs("GET", "/api/v1/system/status", admC)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	var status struct {
		RecDownCount int `json:"recDownCount"`
		Cameras      []struct {
			CameraID          string `json:"cameraId"`
			RecordingExpected bool   `json:"recordingExpected"`
			RecordingOk       bool   `json:"recordingOk"`
			RecordingState    string `json:"recordingState"`
		} `json:"cameras"`
	}
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &status))

	want := map[string]struct {
		expected, ok bool
		state        string
	}{
		"idle":     {false, false, "notScheduled"},
		"broken":   {true, false, "failed"},
		"working":  {true, true, "recording"},
		"never":    {false, false, "off"},
		"disabled": {false, false, "off"},
	}
	for _, c := range status.Cameras {
		w := want[c.CameraID]
		if c.RecordingExpected != w.expected || c.RecordingOk != w.ok || c.RecordingState != w.state {
			t.Errorf("%s: expected=%v ok=%v state=%q, want %+v", c.CameraID, c.RecordingExpected, c.RecordingOk, c.RecordingState, w)
		}
	}
	if len(status.Cameras) != len(want) {
		t.Errorf("%d cameras reported, want %d", len(status.Cameras), len(want))
	}
	if status.RecDownCount != 1 {
		t.Errorf("recDownCount = %d, want only the broken camera", status.RecDownCount)
	}

	// The REC badge on a tile comes from the camera list.
	rec = s.reqAs("GET", "/api/v1/cameras", admC)
	var cams []struct {
		ID           string `json:"id"`
		RecordingNow bool   `json:"recordingNow"`
	}
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &cams))
	wantNow := map[string]bool{"idle": false, "broken": true, "working": true, "never": false, "disabled": false}
	for _, c := range cams {
		if c.RecordingNow != wantNow[c.ID] {
			t.Errorf("%s: recordingNow = %v", c.ID, c.RecordingNow)
		}
	}

	// A schedule flipping must not change the ETag: every edit is checked against it.
	first := s.reqAs("GET", "/api/v1/cameras/idle", admC)
	mustDo(t, s.st.SetSchedules("idle", allWeek))
	second := s.reqAs("GET", "/api/v1/cameras/idle", admC)
	if e1, e2 := first.Header().Get("ETag"), second.Header().Get("ETag"); e1 == "" || e1 != e2 {
		t.Errorf("ETag moved with the schedule: %q -> %q", e1, e2)
	}
	var one struct {
		RecordingNow bool `json:"recordingNow"`
	}
	mustDo(t, json.Unmarshal(second.Body.Bytes(), &one))
	if !one.RecordingNow {
		t.Error("recordingNow should follow the schedule")
	}
}
