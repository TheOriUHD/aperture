package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/notify"
	"github.com/TheOriUHD/aperture/internal/store"
)

var channelTypes = map[string]bool{
	"webpush": true, "ntfy": true, "telegram": true, "discord": true, "webhook": true, "email": true,
}

func channelView(c *store.NotifyChannel) map[string]any {
	return map[string]any{
		"id": c.ID, "type": c.Type, "name": c.Name, "enabled": c.Enabled, "createdAt": c.CreatedAt,
	}
}

// --- channels ------------------------------------------------------------

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	chans, err := s.st.ListChannelsByUser(c.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(chans))
	for _, ch := range chans {
		out = append(out, channelView(ch))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type   string            `json:"type"`
		Name   string            `json:"name"`
		Config map[string]string `json:"config"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !channelTypes[req.Type] {
		writeError(w, http.StatusBadRequest, "unknown channel type")
		return
	}
	cfg, _ := json.Marshal(req.Config)
	c := auth.FromContext(r.Context())
	name := req.Name
	if name == "" {
		name = req.Type
	}
	ch := &store.NotifyChannel{
		UserID: c.UserID, Type: req.Type, Name: name,
		Config: string(cfg), Enabled: true, CreatedAt: time.Now().Unix(),
	}
	if err := s.st.CreateChannel(ch); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create channel")
		return
	}
	s.logAudit(r, "notify.channel.create", name, req.Type)
	writeJSON(w, http.StatusCreated, channelView(ch))
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	ch, err := s.st.GetChannel(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.isOwnerOrAdmin(r, ch.UserID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	var req struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != "" {
		ch.Name = req.Name
	}
	ch.Enabled = req.Enabled
	if err := s.st.UpdateChannel(ch); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, channelView(ch))
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	ch, err := s.st.GetChannel(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.isOwnerOrAdmin(r, ch.UserID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	_ = s.st.DeleteChannel(ch.ID)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleTestChannel(w http.ResponseWriter, r *http.Request) {
	ch, err := s.st.GetChannel(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.isOwnerOrAdmin(r, ch.UserID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	if err := s.notify.TestSend(ch); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sent"})
}

// --- rules ---------------------------------------------------------------

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	rules, err := s.st.ListRulesByUser(c.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if rules == nil {
		rules = []*store.NotifyRule{}
	}
	writeJSON(w, http.StatusOK, rules)
}

type ruleReq struct {
	Name       string               `json:"name"`
	Enabled    bool                 `json:"enabled"`
	Cameras    []string             `json:"cameras"`
	EventTypes []string             `json:"eventTypes"`
	Channels   []string             `json:"channels"`
	QuietStart int                  `json:"quietStart"`
	QuietEnd   int                  `json:"quietEnd"`
	CooldownS  int                  `json:"cooldownSec"`
	Conditions store.RuleConditions `json:"conditions"`
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var req ruleReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c := auth.FromContext(r.Context())
	rule := &store.NotifyRule{
		UserID: c.UserID, Name: req.Name, Enabled: req.Enabled,
		Cameras: req.Cameras, EventTypes: req.EventTypes, Channels: req.Channels,
		QuietStart: req.QuietStart, QuietEnd: req.QuietEnd, CooldownS: req.CooldownS,
		Conditions: req.Conditions,
		CreatedAt:  time.Now().Unix(),
	}
	if rule.QuietStart == 0 && rule.QuietEnd == 0 {
		rule.QuietStart, rule.QuietEnd = -1, -1
	}
	if err := s.st.CreateRule(rule); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create rule")
		return
	}
	s.logAudit(r, "notify.rule.create", req.Name, "")
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	rule, err := s.st.GetRule(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.isOwnerOrAdmin(r, rule.UserID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	var req ruleReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rule.Name, rule.Enabled = req.Name, req.Enabled
	rule.Cameras, rule.EventTypes, rule.Channels = req.Cameras, req.EventTypes, req.Channels
	rule.QuietStart, rule.QuietEnd, rule.CooldownS = req.QuietStart, req.QuietEnd, req.CooldownS
	rule.Conditions = req.Conditions
	if err := s.st.UpdateRule(rule); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	rule, err := s.st.GetRule(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.isOwnerOrAdmin(r, rule.UserID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	_ = s.st.DeleteRule(rule.ID)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleTestRule(w http.ResponseWriter, r *http.Request) {
	rule, err := s.st.GetRule(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.isOwnerOrAdmin(r, rule.UserID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	var lastErr string
	sent := 0
	for _, id := range rule.Channels {
		ch, err := s.st.GetChannel(id)
		if err != nil {
			continue
		}
		if err := s.notify.TestSend(ch); err != nil {
			lastErr = err.Error()
		} else {
			sent++
		}
	}
	if sent == 0 && lastErr != "" {
		writeError(w, http.StatusBadGateway, lastErr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent})
}

// --- web push ------------------------------------------------------------

func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"publicKey": s.notify.VAPIDPublic()})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Endpoint == "" {
		writeError(w, http.StatusBadRequest, "invalid subscription")
		return
	}
	c := auth.FromContext(r.Context())
	err := s.st.UpsertPushSub(&store.PushSubscription{
		UserID: c.UserID, Endpoint: req.Endpoint, P256dh: req.Keys.P256dh, Auth: req.Keys.Auth,
		UA: r.UserAgent(), CreatedAt: time.Now().Unix(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save subscription")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	_ = s.st.DeletePushSubByEndpoint(req.Endpoint)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// --- public signed thumbnail --------------------------------------------

func (s *Server) handlePublicThumb(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	exp, _ := strconv.ParseInt(q.Get("exp"), 10, 64)
	if !notify.VerifyThumb(s.cfg.StreamSecret, q.Get("e"), q.Get("sig"), exp) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ev, err := s.st.EventByID(q.Get("e"))
	if err != nil || ev.ThumbPath == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, ev.ThumbPath)
}
