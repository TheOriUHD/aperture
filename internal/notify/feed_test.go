package notify

import (
	"encoding/json"
	"testing"
)

// TestFeedHubAuthzAndFilter covers per-message camera authorization and the
// type/camera filters in the fan-out.
func TestFeedHubAuthzAndFilter(t *testing.T) {
	e := &Engine{}
	// Subscriber may see cam1 only, no type/camera filter.
	_, ch := e.SubscribeFeed(FeedFilter{}, func(cam string) bool { return cam == "cam1" })

	e.PublishFeed(FeedMessage{ID: 1, Type: "event.created", Camera: "cam1", Durable: true})
	e.PublishFeed(FeedMessage{ID: 2, Type: "event.created", Camera: "cam2", Durable: true}) // withheld

	select {
	case f := <-ch:
		if f.ID != 1 {
			t.Fatalf("first frame id = %d, want 1 (cam1)", f.ID)
		}
	default:
		t.Fatal("expected a cam1 frame")
	}
	select {
	case f := <-ch:
		t.Fatalf("cam2 frame id %d leaked to a cam1-only subscriber", f.ID)
	default: // correct: cam2 was withheld by authz
	}
}

// TestFeedHubTypeFilter covers server-side ?types filtering.
func TestFeedHubTypeFilter(t *testing.T) {
	e := &Engine{}
	_, ch := e.SubscribeFeed(FeedFilter{Types: map[string]bool{"camera.state": true}}, nil)

	e.PublishFeed(FeedMessage{Type: "event.created", Camera: "cam1", Durable: true}) // filtered out
	e.PublishFeed(FeedMessage{Type: "camera.state", Camera: "cam1"})                 // ephemeral, passes

	select {
	case f := <-ch:
		var got map[string]any
		_ = json.Unmarshal(f.Data, &got)
		if got["type"] != "camera.state" {
			t.Fatalf("got type %v, want camera.state", got["type"])
		}
	default:
		t.Fatal("expected the camera.state frame")
	}
	select {
	case <-ch:
		t.Fatal("event.created leaked past the type filter")
	default:
	}
}

// TestFeedEphemeralSnapshot covers the snapshot cache: the latest ephemeral
// message per (type,camera) is retained and authorized on read.
func TestFeedEphemeralSnapshot(t *testing.T) {
	e := &Engine{}
	e.PublishFeed(FeedMessage{Type: "camera.state", Camera: "cam1", Data: "online"})
	e.PublishFeed(FeedMessage{Type: "camera.state", Camera: "cam1", Data: "offline"}) // supersedes
	e.PublishFeed(FeedMessage{Type: "camera.state", Camera: "cam2", Data: "online"})

	// Admin authz (nil-like: allow all) sees both cameras, latest state each.
	snap := e.EphemeralSnapshot(FeedFilter{}, func(string) bool { return true })
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2 (one per camera)", len(snap))
	}
	for _, m := range snap {
		if m.Camera == "cam1" && m.Data != "offline" {
			t.Errorf("cam1 snapshot = %v, want the latest (offline)", m.Data)
		}
	}

	// A cam1-only viewer never sees cam2 in the snapshot.
	only1 := e.EphemeralSnapshot(FeedFilter{}, func(cam string) bool { return cam == "cam1" })
	if len(only1) != 1 || only1[0].Camera != "cam1" {
		t.Fatalf("cam1-only snapshot = %+v, want just cam1", only1)
	}
}
