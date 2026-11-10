package ha

import (
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

func TestPresenceEntities(t *testing.T) {
	p, st, fc, _ := newTestPublisher(t)
	for _, c := range []*store.Camera{
		{ID: "door", Name: "Door", Enabled: true, RecordMode: store.ModeNever, RetentionDays: 7},
		{ID: "attic", Name: "Attic", Enabled: true, RecordMode: store.ModeContinuous, RetentionDays: 7},
	} {
		if err := st.CreateCamera(c); err != nil {
			t.Fatal(err)
		}
	}
	now := map[string]Presence{"door": {Person: true, People: 2, Vehicle: true, Parked: true, Packages: true}}
	var kept map[string]bool
	moving := map[string]bool{"door": true}
	p.SetPresence(func(id string) (Presence, bool) {
		pr, ok := now[id] // the attic has no recognition
		return pr, ok
	}, func(ids map[string]bool) { kept = ids }, func(id string) bool { return moving[id] })

	p.publishDiscovery()
	cfg := func(component, id, key string) string {
		return text(fc.published["homeassistant/"+component+"/aperture_"+id+"_"+key+"/config"].payload)
	}
	for _, e := range presenceEntities {
		if c := cfg(e.component, "door", e.key); !strings.Contains(c, `"unique_id":"aperture_door_`+e.key+`"`) {
			t.Errorf("door %s: config %q", e.key, c)
		}
		if c := cfg(e.component, "attic", e.key); c != "" {
			t.Errorf("attic (no recognition) got a %s entity: %q", e.key, c)
		}
	}
	if !strings.Contains(cfg("binary_sensor", "door", "person"), `"device_class":"occupancy"`) {
		t.Error("Person is not an occupancy sensor")
	}

	state := func(id, key string) string { return text(fc.published["aperture/"+id+"/"+key].payload) }
	p.publishPresence()
	if state("door", "person") != "ON" || state("door", "people") != "2" || state("door", "parked") != "ON" ||
		state("door", "animal") != "OFF" || state("door", "package") != "OFF" {
		t.Fatalf("door states: person %s people %s parked %s animal %s package %s", state("door", "person"),
			state("door", "people"), state("door", "parked"), state("door", "animal"), state("door", "package"))
	}

	// Only what changed is sent again.
	before := len(fc.published)
	fc.published = map[string]published{}
	now["door"] = Presence{Person: false, People: 0, Vehicle: true, Parked: true, Package: true, Packages: true}
	p.publishPresence()
	if _, ok := fc.published["aperture/door/vehicle"]; ok {
		t.Error("an unchanged state was sent again")
	}
	if state("door", "person") != "OFF" || state("door", "people") != "0" || state("door", "package") != "ON" {
		t.Errorf("after they left: person %s people %s package %s", state("door", "person"), state("door", "people"), state("door", "package"))
	}
	_ = before

	// Motion follows live motion too: the door records nothing, so it has no events.
	p.publishMotionRecording()
	if state("door", "motion") != "ON" {
		t.Error("a camera that records nothing never reports motion")
	}

	// While connected, recognition is asked to look at the published cameras;
	// once disconnected, at none.
	p.keepLooking(p.publishedCameraIDs())
	if !kept["door"] || !kept["attic"] {
		t.Errorf("kept = %v", kept)
	}
	p.keepLooking(nil)
	if len(kept) != 0 {
		t.Errorf("still kept after disconnect: %v", kept)
	}
}

// text is a published payload as a string.
func text(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	}
	return ""
}
