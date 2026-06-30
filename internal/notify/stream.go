package notify

import (
	"encoding/json"

	"github.com/TheOriUHD/aperture/internal/store"
)

// subscriber is one connected SSE client (a logged-in device session).
type subscriber struct {
	userID string
	ch     chan []byte
}

// Subscribe registers a live-notification subscriber for a user and returns its
// id (for Unsubscribe) and a receive-only channel of JSON event payloads.
func (e *Engine) Subscribe(userID string) (int, <-chan []byte) {
	e.subMu.Lock()
	defer e.subMu.Unlock()
	if e.subs == nil {
		e.subs = map[int]*subscriber{}
	}
	e.nextSub++
	id := e.nextSub
	sub := &subscriber{userID: userID, ch: make(chan []byte, 16)}
	e.subs[id] = sub
	return id, sub.ch
}

// Unsubscribe drops a subscriber and closes its channel.
func (e *Engine) Unsubscribe(id int) {
	e.subMu.Lock()
	defer e.subMu.Unlock()
	if sub, ok := e.subs[id]; ok {
		close(sub.ch)
		delete(e.subs, id)
	}
}

// broadcast pushes a device-notification payload to every subscriber that is
// allowed to see the camera. Non-blocking: a backed-up subscriber drops the
// event rather than stalling the detector.
func (e *Engine) broadcast(ev *store.Event, cam *store.Camera) {
	// Snapshot the distinct user IDs so the ACL lookups (DB queries) run without
	// holding the lock.
	e.subMu.Lock()
	if len(e.subs) == 0 {
		e.subMu.Unlock()
		return
	}
	users := make(map[string]struct{}, len(e.subs))
	for _, s := range e.subs {
		users[s.userID] = struct{}{}
	}
	e.subMu.Unlock()

	allowed := make(map[string]bool, len(users))
	for uid := range users {
		allowed[uid] = e.userAllowed(uid, ev.CameraID)
	}

	note := e.buildNote(ev, cam)
	data, err := json.Marshal(map[string]any{
		"cameraId": ev.CameraID,
		"camera":   cam.Name,
		"type":     ev.Type,
		"ts":       ev.StartTS,
		"title":    note.Title,
		"body":     note.Body,
		"url":      note.URL,
		"thumbUrl": note.Image,
	})
	if err != nil {
		return
	}

	// Send under the lock so a concurrent Unsubscribe can't close a channel
	// mid-send.
	e.subMu.Lock()
	defer e.subMu.Unlock()
	for _, s := range e.subs {
		if !allowed[s.userID] {
			continue
		}
		select {
		case s.ch <- data:
		default: // subscriber is slow; drop this event for them
		}
	}
}
