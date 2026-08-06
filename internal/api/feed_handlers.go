package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/notify"
)

// emitConfig publishes a config.updated change-feed message for a domain
// ("site", "detection", "schedule", …) after a successful config mutation.
func (s *Server) emitConfig(domain string) {
	if s.notify != nil {
		s.notify.PublishFeed(notify.FeedMessage{
			Type: "config.updated", TS: time.Now().UnixMilli(),
			Data: map[string]any{"domain": domain},
		})
	}
}

// emitStorage publishes a storage.updated change-feed message after a storage
// config or allocation change (lets the web drop its storage poll).
func (s *Server) emitStorage() {
	if s.notify != nil {
		s.notify.PublishFeed(notify.FeedMessage{Type: "storage.updated", TS: time.Now().UnixMilli()})
	}
}

// emitAutomation publishes an automation.updated change-feed message after an
// automation/rule/channel change.
func (s *Server) emitAutomation() {
	if s.notify != nil {
		s.notify.PublishFeed(notify.FeedMessage{Type: "automation.updated", TS: time.Now().UnixMilli()})
	}
}

// handleFeed is the v1 change feed (T2.5): a Server-Sent-Events stream of
// {id,type,ts,camera,data} envelopes. On (re)connect it (1) replays durable
// messages the client missed — events with seq > Last-Event-ID, straight from
// the DB — or sends `reset` if that cursor predates the oldest event; (2) sends
// a snapshot of current ephemeral state; then (3) streams live. Every message is
// re-authorized against the connection's cached camera set, and a token
// credential is re-resolved every 60s so a revoked token stops within ≤60s.
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Cache the camera authorization once for the life of the connection.
	isAdmin, camSet := s.allowedCameraSet(r)
	authz := func(cam string) bool { return isAdmin || camSet[cam] }
	var replayCams []string // nil = all (admin)
	if !isAdmin {
		replayCams = make([]string, 0, len(camSet))
		for id := range camSet {
			replayCams = append(replayCams, id)
		}
	}

	filter := parseFeedFilter(r)
	id, ch := s.notify.SubscribeFeed(filter, authz)
	defer s.notify.UnsubscribeFeed(id)

	fmt.Fprint(w, ": connected\n\n")

	// (1) Resume: replay durable events since the client's Last-Event-ID.
	if last := lastEventID(r); last > 0 {
		if oldest, err := s.st.OldestEventSeq(); err == nil && oldest > 0 && last < oldest-1 {
			// The client's cursor predates the oldest retained event: it can't be
			// resumed, so tell it to refetch from scratch.
			writeFeedFrame(w, notify.FeedFrame{Data: []byte(`{"type":"reset"}`)})
		} else if evs, err := s.st.EventsSinceSeq(last, 1000, replayCams); err == nil {
			for _, ev := range evs {
				m := notify.FeedMessage{
					ID: ev.Seq, Type: "event.created", TS: ev.StartTS, Camera: ev.CameraID,
					Data: map[string]any{"eventId": ev.ID, "eventType": ev.Type, "score": ev.Score},
				}
				if !filter.Match(m) {
					continue
				}
				data, _ := json.Marshal(m)
				writeFeedFrame(w, notify.FeedFrame{Data: data, ID: ev.Seq, Durable: true})
			}
		}
	}

	// (2) Snapshot of current ephemeral state.
	for _, m := range s.notify.EphemeralSnapshot(filter, authz) {
		data, _ := json.Marshal(m)
		writeFeedFrame(w, notify.FeedFrame{Data: data})
	}
	flusher.Flush()

	// (3) Live stream, with keepalive and a token-revocation re-check.
	bearer, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	ka := time.NewTicker(25 * time.Second)
	defer ka.Stop()
	revoke := time.NewTicker(60 * time.Second)
	defer revoke.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-revoke.C:
			// A token deleted/expired mid-stream is caught within ≤60s. Sessions
			// and the CLI token carry standing authority and are not re-resolved.
			if c.Class == auth.ClassToken && bearer != "" && s.am.APIToken(bearer) == nil {
				return
			}
		case <-ka.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case frame, open := <-ch:
			if !open {
				return
			}
			writeFeedFrame(w, frame)
			flusher.Flush()
		}
	}
}

// writeFeedFrame writes one SSE frame. Only durable frames carry an `id:` line,
// so the client's Last-Event-ID always tracks the last durable seq (ephemeral
// ids are in-process and meaningless across a restart).
func writeFeedFrame(w http.ResponseWriter, f notify.FeedFrame) {
	if f.Durable {
		fmt.Fprintf(w, "id: %d\n", f.ID)
	}
	fmt.Fprintf(w, "data: %s\n\n", f.Data)
}

// parseFeedFilter reads ?types and ?cameras (comma-separated) into a filter.
func parseFeedFilter(r *http.Request) notify.FeedFilter {
	return notify.FeedFilter{
		Types:   csvSet(r.URL.Query().Get("types")),
		Cameras: csvSet(r.URL.Query().Get("cameras")),
	}
}

func csvSet(s string) map[string]bool {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	out := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out[p] = true
		}
	}
	return out
}

// lastEventID reads the resume cursor: the EventSource Last-Event-ID header on
// reconnect, or a ?lastEventId query param on the first connect.
func lastEventID(r *http.Request) int64 {
	v := r.Header.Get("Last-Event-ID")
	if v == "" {
		v = r.URL.Query().Get("lastEventId")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return 0
	}
	return n
}
