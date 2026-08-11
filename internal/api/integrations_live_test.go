package api

import (
	"net/http/httptest"
	"testing"

	"github.com/TheOriUHD/aperture/internal/store"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Zimmer PH":      "zimmer_ph",
		"A1 Mini":        "a1_mini",
		"Zimmer Anna": "zimmer_anna",
		"  Front  Door ": "front_door",
		"H2C":            "h2c",
		"":               "",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveCameraID(t *testing.T) {
	s := newTestServer(t)
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "abc123", Name: "Zimmer PH"}))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "def456", Name: "Front Door"}))

	// Real id resolves to itself.
	if id, ok := s.resolveCameraID("abc123"); !ok || id != "abc123" {
		t.Errorf("resolve by id: got (%q,%v), want (abc123,true)", id, ok)
	}
	// Slug resolves to the real id.
	if id, ok := s.resolveCameraID("zimmer_ph"); !ok || id != "abc123" {
		t.Errorf("resolve by slug: got (%q,%v), want (abc123,true)", id, ok)
	}
	if id, ok := s.resolveCameraID("front_door"); !ok || id != "def456" {
		t.Errorf("resolve front_door: got (%q,%v), want (def456,true)", id, ok)
	}
	// Unknown → not found.
	if _, ok := s.resolveCameraID("nope"); ok {
		t.Error("resolve unknown slug should fail")
	}
	if _, ok := s.resolveCameraID(""); ok {
		t.Error("resolve empty should fail")
	}
}

// TestLiveKeyAuthorizes pins the managed-live-key WS auth: the current key opens ANY
// camera's live socket, a wrong/empty value never does, and a reroll instantly kills the
// old value while the new one works.
func TestLiveKeyAuthorizes(t *testing.T) {
	s := newTestServer(t)
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam1", Name: "1"}))
	mustDo(t, s.st.CreateCamera(&store.Camera{ID: "cam2", Name: "2"}))

	authOK := func(cam, token string) bool {
		r := httptest.NewRequest("GET", "/api/v1/cameras/"+cam+"/ws", nil)
		_, ok := s.authorizeStreamWS(r, cam, token)
		return ok
	}

	// No key minted yet → nothing authorizes via the live-key path.
	if authOK("cam1", "aplv_anything") {
		t.Fatal("no live key exists yet; nothing should authorize")
	}

	k1, err := s.rollLiveKey()
	mustDo(t, err)
	if !authOK("cam1", k1) || !authOK("cam2", k1) {
		t.Error("the live key must authorize every camera")
	}
	if authOK("cam1", "aplv_wrong") || authOK("cam1", "") {
		t.Error("a wrong/empty value must not authorize")
	}

	// Reroll: old key dies, new key works.
	k2, err := s.rollLiveKey()
	mustDo(t, err)
	if k2 == k1 {
		t.Fatal("reroll must produce a different key")
	}
	if authOK("cam1", k1) {
		t.Error("the old key must stop authorizing after a reroll")
	}
	if !authOK("cam1", k2) {
		t.Error("the new key must authorize")
	}
}
