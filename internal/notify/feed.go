package notify

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

// Change feed (T2.5). A general Server-Sent-Events feed layered on the same hub
// as device notifications. Messages are one of two classes:
//
//   - durable: events/detections. The message ID is the events `seq` (monotonic,
//     unique), so a reconnecting client resumes by replaying durable messages
//     with seq > Last-Event-ID straight from the DB — no server-side ring buffer.
//   - ephemeral: camera/storage/health/… live state. The ID is an in-process
//     monotonic counter; these are NOT replayable, so a fresh snapshot of the
//     latest ephemeral state per (type,camera) is sent on connect instead.
//
// The per-domain emitters (event.*, storage.updated, camera.state, …) are wired
// in Phase 3; this file is the transport they publish through.

// FeedMessage is one change-feed envelope: {id, type, ts, camera, data}.
type FeedMessage struct {
	ID     int64  `json:"id"`
	Type   string `json:"type"` // "<domain>.<action>"
	TS     int64  `json:"ts"`   // unix ms
	Camera string `json:"camera,omitempty"`
	Data   any    `json:"data,omitempty"`
	// Durable marks a replayable message whose ID is a real events seq. Not
	// serialized; the SSE layer only sets the wire `id:` for durable messages so
	// the client's Last-Event-ID always tracks the last durable seq.
	Durable bool `json:"-"`
}

// FeedFilter narrows a subscription server-side. Empty sets match everything.
type FeedFilter struct {
	Types   map[string]bool
	Cameras map[string]bool
}

func (f FeedFilter) Match(m FeedMessage) bool {
	if len(f.Types) > 0 && !f.Types[m.Type] {
		return false
	}
	if len(f.Cameras) > 0 && m.Camera != "" && !f.Cameras[m.Camera] {
		return false
	}
	return true
}

// FeedFrame is a pre-serialized SSE payload plus the metadata the SSE writer
// needs to decide whether to emit an `id:` line.
type FeedFrame struct {
	Data    []byte
	ID      int64
	Durable bool
}

type feedSub struct {
	ch     chan FeedFrame
	filter FeedFilter
	// authz is re-checked per message with the message's camera id — the
	// security boundary. It reflects the connection's cached claims; a camera the
	// subscriber may not see is withheld even if it slips past the filter.
	authz func(camera string) bool
}

// SubscribeFeed registers a change-feed subscriber and returns its id and frame
// channel. authz must return false to withhold a camera the subscriber may not
// see.
func (e *Engine) SubscribeFeed(f FeedFilter, authz func(camera string) bool) (int, <-chan FeedFrame) {
	e.feedMu.Lock()
	defer e.feedMu.Unlock()
	if e.feedSubs == nil {
		e.feedSubs = map[int]*feedSub{}
	}
	e.nextFeed++
	id := e.nextFeed
	sub := &feedSub{ch: make(chan FeedFrame, 64), filter: f, authz: authz}
	e.feedSubs[id] = sub
	return id, sub.ch
}

// UnsubscribeFeed drops a subscriber and closes its channel.
func (e *Engine) UnsubscribeFeed(id int) {
	e.feedMu.Lock()
	defer e.feedMu.Unlock()
	if sub, ok := e.feedSubs[id]; ok {
		close(sub.ch)
		delete(e.feedSubs, id)
	}
}

// PublishFeed fans a message out to matching, authorized subscribers.
// Ephemeral messages are assigned an in-process id and update the snapshot
// cache. Delivery is non-blocking: a backed-up subscriber drops the frame.
func (e *Engine) PublishFeed(m FeedMessage) {
	if !m.Durable {
		m.ID = e.feedSeq.Add(1)
		e.cacheEphemeral(m)
	}
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	frame := FeedFrame{Data: data, ID: m.ID, Durable: m.Durable}

	e.feedMu.Lock()
	defer e.feedMu.Unlock()
	for _, s := range e.feedSubs {
		if !s.filter.Match(m) {
			continue
		}
		if m.Camera != "" && s.authz != nil && !s.authz(m.Camera) {
			continue
		}
		select {
		case s.ch <- frame:
		default: // slow subscriber; drop this frame for them
		}
	}
}

// cacheEphemeral keeps the latest ephemeral message per (type,camera) so a newly
// connected client can be handed a snapshot of current live state.
func (e *Engine) cacheEphemeral(m FeedMessage) {
	e.snapMu.Lock()
	defer e.snapMu.Unlock()
	if e.snapshot == nil {
		e.snapshot = map[string]FeedMessage{}
	}
	e.snapshot[m.Type+"|"+m.Camera] = m
}

// EphemeralSnapshot returns the current ephemeral state matching the filter and
// authorized by authz — sent to a client right after (re)connect.
func (e *Engine) EphemeralSnapshot(f FeedFilter, authz func(camera string) bool) []FeedMessage {
	e.snapMu.Lock()
	defer e.snapMu.Unlock()
	out := make([]FeedMessage, 0, len(e.snapshot))
	for _, m := range e.snapshot {
		if !f.Match(m) {
			continue
		}
		if m.Camera != "" && authz != nil && !authz(m.Camera) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// --- domain emitters (Phase 3) --------------------------------------------
// These are the hooks the detector, detect engine, and watchdog invoke; each
// publishes a change-feed message. event.created is emitted from OnEvent.

// OnEventClose emits a durable event.closed carrying the same seq as its
// event.created, so a resuming client sees the close via Last-Event-ID replay.
func (e *Engine) OnEventClose(eventID string) {
	ev, err := e.st.EventByID(eventID)
	if err != nil {
		return
	}
	e.PublishFeed(FeedMessage{
		ID: ev.Seq, Type: "event.closed", TS: ev.EndTS, Camera: ev.CameraID,
		Data:    map[string]any{"eventId": ev.ID, "endTsMs": ev.EndTS},
		Durable: true,
	})
}

// OnDetections emits detection.created when objects are attached to an event.
// Ephemeral (in-process id): making it durable would set the SSE id: to the
// event's seq and let Last-Event-ID regress when an older event's detections
// land after a newer event.created. The event itself is durable by seq, so a
// resuming client refetches /events/{id}/detections — no data lost.
func (e *Engine) OnDetections(eventID, cameraID string) {
	e.PublishFeed(FeedMessage{
		Type: "detection.created", TS: time.Now().UnixMilli(), Camera: cameraID,
		Data: map[string]any{"eventId": eventID},
	})
}

// OnExportProgress emits an ephemeral export.progress. No camera — an export can
// span several — so it is delivered to every subscriber (progress % isn't
// camera-sensitive); the durable export result is fetched via the export API.
func (e *Engine) OnExportProgress(id string, progress float64) {
	e.PublishFeed(FeedMessage{
		Type: "export.progress", TS: time.Now().UnixMilli(),
		Data: map[string]any{"exportId": id, "progress": progress},
	})
}

// OnCameraState emits an ephemeral camera.state on an online/offline transition.
func (e *Engine) OnCameraState(cameraID string, online bool) {
	state := "offline"
	if online {
		state = "online"
	}
	e.PublishFeed(FeedMessage{
		Type: "camera.state", TS: time.Now().UnixMilli(), Camera: cameraID,
		Data: map[string]any{"state": state},
	})
}

// feed hub state, embedded in Engine.
type feedHub struct {
	feedMu   sync.Mutex
	feedSubs map[int]*feedSub
	nextFeed int
	feedSeq  atomic.Int64

	snapMu   sync.Mutex
	snapshot map[string]FeedMessage
}
