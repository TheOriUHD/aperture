// Package ha integrates Aperture with Home Assistant over MQTT (outbound: HA
// connects to the broker and auto-discovers Aperture's entities). It publishes
// MQTT discovery configs and live state for per-camera motion + recording
// binary sensors, a snapshot camera entity, and NVR storage/event sensors.
package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/store"
)

const (
	statusTopic    = "aperture/status"
	defaultPrefix  = "homeassistant"
	clientID       = "aperture-nvr"
	manufacturer   = "Aperture"
	stateTopicBase = "aperture"
)

type snapshotter interface {
	SnapshotCached(ctx context.Context, cameraID string) ([]byte, error)
}

// Settings is the resolved HA/MQTT configuration.
type Settings struct {
	Enabled  bool
	Broker   string
	Username string
	Password string
	Prefix   string
}

// Publisher pushes Aperture state to an MQTT broker for Home Assistant.
type Publisher struct {
	cfg *config.Config
	st  *store.Store
	sm  snapshotter
	log *slog.Logger

	mu        sync.Mutex
	client    mqtt.Client
	current   Settings
	siteName  func() string
	reconcile func() // re-sync recorder + detector after an inbound record-mode change

	motion    map[string]bool // last published motion state per camera
	recording map[string]bool
}

// SetReconcile wires the callback that applies a record-mode change immediately
// (recorder + detector) when Home Assistant sends a command.
func (p *Publisher) SetReconcile(fn func()) { p.reconcile = fn }

// New constructs a Publisher. siteName returns the configured site name.
func New(cfg *config.Config, st *store.Store, sm snapshotter, siteName func() string, log *slog.Logger) *Publisher {
	return &Publisher{
		cfg: cfg, st: st, sm: sm, siteName: siteName, log: log,
		motion: map[string]bool{}, recording: map[string]bool{},
	}
}

// LoadSettings resolves HA settings from the settings store, with env defaults.
func (p *Publisher) LoadSettings() Settings {
	get := func(k, def string) string {
		if v, err := p.st.GetSetting(k); err == nil && v != "" {
			return v
		}
		return def
	}
	return Settings{
		Enabled:  get("ha_enabled", "false") == "true",
		Broker:   get("ha_broker", p.cfg.MQTTURL),
		Username: get("ha_user", ""),
		Password: get("ha_pass", ""),
		Prefix:   get("ha_prefix", defaultPrefix),
	}
}

// Run maintains the connection and publishes state on a fixed cadence.
func (p *Publisher) Run(ctx context.Context) {
	p.Reconcile()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var n int64
	for {
		select {
		case <-ctx.Done():
			p.disconnect()
			return
		case <-t.C:
			n++
			if n%10 == 0 {
				p.Reconcile() // pick up settings changes / retry connect
			}
			if !p.connected() {
				continue
			}
			if n%2 == 0 {
				p.publishMotionRecording()
			}
			if n%10 == 0 {
				p.publishSnapshots()
			}
			if n%30 == 0 {
				p.publishStorage()
				p.publishControlState()
			}
			if n%300 == 0 {
				p.publishDiscovery() // refresh + pick up new cameras
			}
		}
	}
}

func (p *Publisher) connected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.client != nil && p.client.IsConnected()
}

// Connected reports whether the MQTT client is currently connected.
func (p *Publisher) Connected() bool { return p.connected() }

// Reconcile (re)connects only when settings change or no client exists yet.
// It must NOT recreate a client that already exists — paho handles transient
// reconnects itself, and a duplicate client ID would make the broker kick the
// existing connection (a flap loop).
func (p *Publisher) Reconcile() {
	s := p.LoadSettings()
	p.mu.Lock()
	changed := s != p.current
	hasClient := p.client != nil
	p.current = s
	p.mu.Unlock()

	if !s.Enabled {
		if hasClient {
			p.disconnect()
		}
		return
	}
	if changed {
		p.disconnect()
		p.connect(s)
		return
	}
	if !hasClient {
		p.connect(s)
	}
	// else: a client already exists; let paho auto-reconnect as needed.
}

func (p *Publisher) connect(s Settings) {
	opts := mqtt.NewClientOptions().
		AddBroker(s.Broker).
		SetClientID(clientID).
		SetUsername(s.Username).
		SetPassword(s.Password).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectTimeout(8*time.Second).
		SetWill(statusTopic, "offline", 1, true).
		SetOnConnectHandler(func(c mqtt.Client) {
			p.log.Info("home assistant: connected", "broker", s.Broker)
			p.publishOnline()
			p.publishDiscovery()
			p.publishMotionRecording()
			p.publishStorage()
			p.publishSnapshots()
			p.publishControlState()
			p.subscribeCommands(c)
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			p.log.Warn("home assistant: connection lost", "err", err)
		})

	client := mqtt.NewClient(opts)
	tok := client.Connect()
	go func() {
		_ = tok.WaitTimeout(8 * time.Second)
		if err := tok.Error(); err != nil {
			p.log.Warn("home assistant: connect failed", "broker", s.Broker, "err", err)
		}
	}()
	p.mu.Lock()
	p.client = client
	p.current = s
	p.mu.Unlock()
}

func (p *Publisher) disconnect() {
	p.mu.Lock()
	c := p.client
	p.client = nil
	p.mu.Unlock()
	if c != nil && c.IsConnected() {
		c.Publish(statusTopic, 1, true, "offline")
		c.Disconnect(250)
	}
}

func (p *Publisher) pub(topic string, retained bool, payload any) {
	p.mu.Lock()
	c := p.client
	p.mu.Unlock()
	if c == nil || !c.IsConnected() {
		return
	}
	c.Publish(topic, 1, retained, payload)
}

// PublishRaw publishes an arbitrary payload to a topic (used by the automation
// engine's "mqtt" action). No-op when disconnected.
func (p *Publisher) PublishRaw(topic string, payload []byte) {
	p.pub(topic, false, payload)
}

func (p *Publisher) prefix() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current.Prefix != "" {
		return p.current.Prefix
	}
	return defaultPrefix
}

func (p *Publisher) publishOnline() {
	p.pub(statusTopic, true, "online")
}

func camDevice(cameraID, name string) map[string]any {
	return map[string]any{
		"identifiers":  []string{"aperture_cam_" + cameraID},
		"name":         name,
		"manufacturer": manufacturer,
		"model":        "Camera",
		"via_device":   "aperture_nvr",
	}
}

func (p *Publisher) nvrDevice() map[string]any {
	return map[string]any{
		"identifiers":  []string{"aperture_nvr"},
		"name":         p.siteName(),
		"manufacturer": manufacturer,
		"model":        "NVR",
	}
}

// publishDiscovery (re)announces all entities to Home Assistant.
func (p *Publisher) publishDiscovery() {
	pfx := p.prefix()
	cams, err := p.st.ListCameras()
	if err != nil {
		return
	}
	for _, c := range cams {
		dev := camDevice(c.ID, c.Name)
		motionCfg := map[string]any{
			"name":               "Motion",
			"unique_id":          "aperture_" + c.ID + "_motion",
			"device_class":       "motion",
			"state_topic":        stateTopicBase + "/" + c.ID + "/motion",
			"payload_on":         "ON",
			"payload_off":        "OFF",
			"availability_topic": statusTopic,
			"device":             dev,
		}
		p.pub(fmt.Sprintf("%s/binary_sensor/aperture_%s_motion/config", pfx, c.ID), true, mustJSON(motionCfg))

		recCfg := map[string]any{
			"name":               "Recording",
			"unique_id":          "aperture_" + c.ID + "_recording",
			"device_class":       "running",
			"state_topic":        stateTopicBase + "/" + c.ID + "/recording",
			"payload_on":         "ON",
			"payload_off":        "OFF",
			"availability_topic": statusTopic,
			"device":             dev,
		}
		p.pub(fmt.Sprintf("%s/binary_sensor/aperture_%s_recording/config", pfx, c.ID), true, mustJSON(recCfg))

		camCfg := map[string]any{
			"name":               nil,
			"unique_id":          "aperture_" + c.ID + "_camera",
			"topic":              stateTopicBase + "/" + c.ID + "/snapshot",
			"availability_topic": statusTopic,
			"device":             dev,
		}
		p.pub(fmt.Sprintf("%s/camera/aperture_%s/config", pfx, c.ID), true, mustJSON(camCfg))
	}

	nvr := p.nvrDevice()
	storageCfg := map[string]any{
		"name":                "Storage used",
		"unique_id":           "aperture_storage_used",
		"state_topic":         stateTopicBase + "/system/storage_used_gb",
		"unit_of_measurement": "GB",
		"icon":                "mdi:harddisk",
		"availability_topic":  statusTopic,
		"device":              nvr,
	}
	p.pub(fmt.Sprintf("%s/sensor/aperture_storage_used/config", pfx), true, mustJSON(storageCfg))

	eventsCfg := map[string]any{
		"name":               "Events today",
		"unique_id":          "aperture_events_today",
		"state_topic":        stateTopicBase + "/system/events_today",
		"icon":               "mdi:motion-sensor",
		"availability_topic": statusTopic,
		"device":             nvr,
	}
	p.pub(fmt.Sprintf("%s/sensor/aperture_events_today/config", pfx), true, mustJSON(eventsCfg))

	// Inbound controls: record-mode Select per camera + a system Arm switch.
	p.publishControlDiscovery(pfx, cams)
}

// publishMotionRecording publishes per-camera motion + recording state on change.
func (p *Publisher) publishMotionRecording() {
	cams, err := p.st.ListCameras()
	if err != nil {
		return
	}
	now := time.Now().UnixMilli()
	for _, c := range cams {
		motion := p.cameraInMotion(c.ID, now)
		if p.motion[c.ID] != motion {
			p.motion[c.ID] = motion
			p.pub(stateTopicBase+"/"+c.ID+"/motion", true, onOff(motion))
		}
		rec := c.Enabled && c.RecordMode != store.ModeNever
		if p.recording[c.ID] != rec {
			p.recording[c.ID] = rec
			p.pub(stateTopicBase+"/"+c.ID+"/recording", true, onOff(rec))
		}
	}
}

// cameraInMotion reports whether the camera currently has an active event.
func (p *Publisher) cameraInMotion(cameraID string, now int64) bool {
	evs, err := p.st.ListEvents(cameraID, now-2000, now+2000)
	if err != nil {
		return false
	}
	for _, e := range evs {
		if e.StartTS <= now && (e.EndTS == 0 || e.EndTS >= now) {
			return true
		}
	}
	return false
}

func (p *Publisher) publishSnapshots() {
	cams, err := p.st.ListCameras()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, c := range cams {
		if !c.Enabled {
			continue
		}
		if data, err := p.sm.SnapshotCached(ctx, c.ID); err == nil && len(data) > 0 {
			p.pub(stateTopicBase+"/"+c.ID+"/snapshot", true, data)
		}
	}
}

func (p *Publisher) publishStorage() {
	if bytes, err := p.st.TotalBytes(); err == nil {
		gb := float64(bytes) / (1 << 30)
		p.pub(stateTopicBase+"/system/storage_used_gb", true, fmt.Sprintf("%.1f", gb))
	}
	startOfDay := time.Now().Truncate(24 * time.Hour).UnixMilli()
	if n, err := p.st.CountEventsSince(startOfDay); err == nil {
		p.pub(stateTopicBase+"/system/events_today", true, fmt.Sprintf("%d", n))
	}
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
