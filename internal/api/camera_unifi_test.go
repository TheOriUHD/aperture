package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

// UniFi Protect links carry no login: the path is the stream key, and anyone with
// it can watch the camera. Pasted as Protect hands them out, they must be played
// the way go2rtc needs, never shown back with the key in them, and survive edits.
func TestUniFiProtectCamera(t *testing.T) {
	s := newTestServer(t)
	admin := &store.User{ID: "a1", Email: "a@t", Name: "A", Role: "admin"}
	mustDo(t, s.st.CreateUser(admin))
	adm := cred{"admin", cookieCred(sessionCookieFor(t, s, admin))}
	const mainKey, subKey = "AbCdEfGhIjKlMnOp", "QrStUvWxYz012345"

	rec := s.reqBody("POST", "/api/v1/cameras", adm, strings.NewReader(`{"name":"Garage",
		"rtspMainUrl":"rtsps://192.168.10.1:7441/`+mainKey+`?enableSrtp",
		"rtspSubUrl":"rtsps://192.168.10.1:7441/`+subKey+`?enableSrtp"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if body := rec.Body.String(); strings.Contains(body, mainKey) || strings.Contains(body, subKey) {
		t.Fatalf("the stream key came back to the browser: %s", body)
	}
	var created struct {
		ID         string `json:"id"`
		Main       string `json:"rtspMainUrl"`
		HAPublish  bool   `json:"haPublish"`
		RecordMode string `json:"recordMode"`
	}
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &created))
	if created.Main != "rtspx://192.168.10.1:7441/•••••" {
		t.Errorf("shown as %q", created.Main)
	}

	c, err := s.st.GetCamera(created.ID)
	mustDo(t, err)
	if c.RTSPMainURL != "rtspx://192.168.10.1:7441/"+mainKey || c.RTSPSubURL != "rtspx://192.168.10.1:7441/"+subKey {
		t.Errorf("stored %q / %q, want go2rtc's rtspx form without enableSrtp", c.RTSPMainURL, c.RTSPSubURL)
	}
	if c.RecordMode != store.ModeNever || c.MotionDetection || !c.HAHidden || created.HAPublish {
		t.Errorf("a Protect camera should start live view only: mode=%s motion=%v haHidden=%v", c.RecordMode, c.MotionDetection, c.HAHidden)
	}

	// Saved back exactly as shown, with the switch flipped: the keys must survive.
	rec = s.reqBody("PUT", "/api/v1/cameras/"+created.ID, adm, strings.NewReader(`{"name":"Garage door",
		"rtspMainUrl":"rtspx://192.168.10.1:7441/•••••","rtspSubUrl":"rtspx://192.168.10.1:7441/•••••","haPublish":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"haPublish":true`) {
		t.Errorf("the edit form wouldn't see the switch on: %s", rec.Body)
	}
	c, err = s.st.GetCamera(created.ID)
	mustDo(t, err)
	if c.RTSPMainURL != "rtspx://192.168.10.1:7441/"+mainKey || c.RTSPSubURL != "rtspx://192.168.10.1:7441/"+subKey {
		t.Errorf("after an edit: %q / %q, the saved keys were lost", c.RTSPMainURL, c.RTSPSubURL)
	}
	if c.HAHidden {
		t.Error("haPublish true didn't put the camera into Home Assistant")
	}

	// Pasting a fresh Protect link into an existing camera is rewritten the same way.
	rec = s.reqBody("PUT", "/api/v1/cameras/"+created.ID, adm, strings.NewReader(`{"rtspMainUrl":"rtsps://192.168.10.1:7441/NewKeyNewKey1234?enableSrtp"}`))
	if c, err = s.st.GetCamera(created.ID); err != nil || c.RTSPMainURL != "rtspx://192.168.10.1:7441/NewKeyNewKey1234" {
		t.Errorf("a pasted link on edit was stored as %q (%d)", c.RTSPMainURL, rec.Code)
	}

	// A masked key is never sent to another address.
	rec = s.reqBody("PUT", "/api/v1/cameras/"+created.ID, adm, strings.NewReader(`{"rtspMainUrl":"rtspx://203.0.113.9:7441/•••••"}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("moving a saved key to another host: %d, want 400", rec.Code)
	}

	// Asked for explicitly, a Protect camera records and is published like any other.
	rec = s.reqBody("POST", "/api/v1/cameras", adm, strings.NewReader(`{"name":"Gate",
		"rtspMainUrl":"rtsps://192.168.10.1:7441/ZyXwVuTsRqPoNmLk?enableSrtp",
		"recordMode":"continuous","motionDetection":true,"haPublish":true}`))
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &created))
	if c, err = s.st.GetCamera(created.ID); err != nil || c.RecordMode != store.ModeContinuous || !c.MotionDetection || c.HAHidden {
		t.Errorf("explicit choices were overridden: %+v %v", c, err)
	}

	// Any other camera keeps the defaults it always had.
	rec = s.reqBody("POST", "/api/v1/cameras", adm, strings.NewReader(`{"name":"Door","rtspMainUrl":"rtsp://u:p@10.0.0.5:554/stream1"}`))
	mustDo(t, json.Unmarshal(rec.Body.Bytes(), &created))
	if c, err = s.st.GetCamera(created.ID); err != nil || c.RecordMode != store.ModeContinuous || !c.MotionDetection || c.HAHidden || c.RTSPMainURL != "rtsp://u:p@10.0.0.5:554/stream1" {
		t.Errorf("a regular camera's defaults changed: %+v %v", c, err)
	}
}
