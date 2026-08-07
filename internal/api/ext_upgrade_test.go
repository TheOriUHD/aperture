package api

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestExtSideloadUpgrade covers installing a bundle whose id is already installed: it
// becomes an UPDATE (not a 409), preserving ext_kv (settings, token, arbitrary storage)
// and the camera grant, snapshotting the previous bundle for revert, reconciling the
// settings schema, and refusing a downgrade without explicit confirmation.
func TestExtSideloadUpgrade(t *testing.T) {
	s, adminC := newExtAdmin(t)
	if rec := s.reqBody("POST", "/api/v1/ext/gate", adminC, strings.NewReader(`{"confirm":"enable extensions"}`)); rec.Code != 200 {
		t.Fatalf("gate: %d", rec.Code)
	}

	v1 := `{"manifest":1,"id":"com.up","name":"Up","version":"1.0.0","ui":{"panels":[{"slot":"settings","entry":"ui/p.html"}]},
	  "settings":[{"key":"keep","label":"Keep","type":"text"},{"key":"drop","label":"Drop","type":"text"},{"key":"retype","label":"Retype","type":"text"}]}`
	v2 := `{"manifest":1,"id":"com.up","name":"Up","version":"2.0.0","ui":{"panels":[{"slot":"settings","entry":"ui/p.html"}]},
	  "settings":[{"key":"keep","label":"Keep","type":"text"},{"key":"retype","label":"Retype","type":"number"},{"key":"added","label":"Added","type":"text","default":"seeded"}]}`

	stage := func(manifest string) (stagingID string, update bool, current string, downgrade bool) {
		rec := do(s, uploadBundle(t, "/api/v1/ext/install", adminC, zipBundle(t, map[string]string{"manifest.json": manifest, "ui/p.html": "<html>x</html>"})))
		if rec.Code != 200 {
			t.Fatalf("stage: %d (%s)", rec.Code, rec.Body)
		}
		var v struct {
			StagingID      string `json:"stagingId"`
			Update         bool   `json:"update"`
			CurrentVersion string `json:"currentVersion"`
			Downgrade      bool   `json:"downgrade"`
		}
		mustDo(t, json.Unmarshal(rec.Body.Bytes(), &v))
		return v.StagingID, v.Update, v.CurrentVersion, v.Downgrade
	}
	confirm := func(id, body string) int {
		return s.reqBody("POST", "/api/v1/ext/install/confirm", adminC, strings.NewReader(body)).Code
	}

	// Install v1 (not flagged as an update).
	sid, upd, _, _ := stage(v1)
	if upd {
		t.Fatal("a first install must not be flagged as an update")
	}
	if code := confirm("", `{"stagingId":"`+sid+`"}`); code != 201 {
		t.Fatalf("install v1: %d", code)
	}

	// The user's stored data: settings, an arbitrary storage key, and a camera grant.
	mustDo(t, s.st.ExtKVSet("com.up", "keep", "KEEPVAL"))
	mustDo(t, s.st.ExtKVSet("com.up", "drop", "DROPVAL"))
	mustDo(t, s.st.ExtKVSet("com.up", "retype", "5"))
	mustDo(t, s.st.ExtKVSet("com.up", "myown", "ARBITRARY")) // not a setting
	mustDo(t, s.st.SetExtensionCameraGrants("com.up", []string{"cam1"}))

	// Upgrade to v2 via the SAME install path — flagged as an update from 1.0.0.
	sid, upd, cur, down := stage(v2)
	if !upd || cur != "1.0.0" || down {
		t.Fatalf("v2 stage should be update from 1.0.0 (not downgrade): upd=%v cur=%q down=%v", upd, cur, down)
	}
	if code := confirm("", `{"stagingId":"`+sid+`"}`); code != 200 {
		t.Fatalf("upgrade confirm should 200 (not 409), got %d", code)
	}

	ex, err := s.st.GetExtension("com.up")
	mustDo(t, err)
	if ex.Version != "2.0.0" {
		t.Fatalf("version not bumped: %s", ex.Version)
	}
	if !strsEq(ex.CameraGrants, []string{"cam1"}) {
		t.Fatalf("camera grant must be preserved across update, got %v", ex.CameraGrants)
	}
	if _, err := os.Stat(s.extPrevDir("com.up")); err != nil {
		t.Fatalf("a revert snapshot must exist after update: %v", err)
	}

	// Settings reconcile: same-type kept, removed dropped, type-changed dropped,
	// arbitrary storage preserved, a new field's default seeded.
	val := func(k string) (string, bool) { v, f, _ := s.st.ExtKVGet("com.up", k); return v, f }
	if v, _ := val("keep"); v != "KEEPVAL" {
		t.Errorf("a same-type setting must keep its value, got %q", v)
	}
	if _, f := val("drop"); f {
		t.Errorf("a removed setting must be dropped")
	}
	if _, f := val("retype"); f {
		t.Errorf("a type-changed setting (text→number) must be dropped")
	}
	if v, _ := val("myown"); v != "ARBITRARY" {
		t.Errorf("non-setting storage must be preserved, got %q", v)
	}
	if v, _ := val("added"); v != "seeded" {
		t.Errorf("a new setting's default must be seeded, got %q", v)
	}

	// A downgrade (v1 over v2) is refused unless explicitly confirmed.
	sid, upd, _, down = stage(v1)
	if !upd || !down {
		t.Fatalf("v1 over v2 must be flagged update+downgrade: upd=%v down=%v", upd, down)
	}
	if code := confirm("", `{"stagingId":"`+sid+`"}`); code != 409 {
		t.Fatalf("downgrade without confirm should 409, got %d", code)
	}
	if code := confirm("", `{"stagingId":"`+sid+`","allowDowngrade":true}`); code != 200 {
		t.Fatalf("downgrade WITH confirm should 200, got %d", code)
	}
	if ex, _ := s.st.GetExtension("com.up"); ex.Version != "1.0.0" {
		t.Fatalf("confirmed downgrade did not apply: %s", ex.Version)
	}
}

// TestExtUpdateApplyRefusesIdMismatch pins that the URL-update path never applies a
// bundle whose id differs from the extension it targets.
func TestExtUpdateApplyRefusesIdMismatch(t *testing.T) {
	s, adminC := newExtAdmin(t)
	s.reqBody("POST", "/api/v1/ext/gate", adminC, strings.NewReader(`{"confirm":"enable extensions"}`))

	base := `{"manifest":1,"id":"com.host","name":"H","version":"1.0.0","ui":{"panels":[{"slot":"settings","entry":"ui/p.html"}]}}`
	rec := do(s, uploadBundle(t, "/api/v1/ext/install", adminC, zipBundle(t, map[string]string{"manifest.json": base, "ui/p.html": "<html>x</html>"})))
	var st struct {
		StagingID string `json:"stagingId"`
	}
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &st))
	s.reqBody("POST", "/api/v1/ext/install/confirm", adminC, strings.NewReader(`{"stagingId":"`+st.StagingID+`"}`))

	// Stage a FOREIGN-id bundle and try to apply it as an update to com.host.
	foreign := `{"manifest":1,"id":"com.other","name":"O","version":"2.0.0","ui":{"panels":[{"slot":"settings","entry":"ui/p.html"}]}}`
	rec = do(s, uploadBundle(t, "/api/v1/ext/install", adminC, zipBundle(t, map[string]string{"manifest.json": foreign, "ui/p.html": "<html>y</html>"})))
	var fs struct {
		StagingID string `json:"stagingId"`
	}
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &fs))
	if rec := s.reqBody("POST", "/api/v1/ext/com.host/update", adminC, strings.NewReader(`{"stagingId":"`+fs.StagingID+`","consented":true}`)); rec.Code != 400 {
		t.Fatalf("applying a com.other bundle to com.host should 400, got %d (%s)", rec.Code, rec.Body)
	}
}
