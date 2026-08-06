package ha

import (
	"context"
	"fmt"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/TheOriUHD/aperture/internal/store"
)

// Inbound control (§14.2): Home Assistant (or anything on the broker) can drive
// Aperture by writing to command topics. We authorize implicitly by requiring
// broker access (the broker itself is the trust boundary).

// subscribeCommands wires the inbound command topics on (re)connect.
func (p *Publisher) subscribeCommands(c mqtt.Client) {
	c.Subscribe(stateTopicBase+"/+/set/record_mode", 1, p.onCommand)
	c.Subscribe(stateTopicBase+"/system/set/armed", 1, p.onCommand)
	c.Subscribe(stateTopicBase+"/+/snapshot/trigger", 1, p.onCommand)
}

func (p *Publisher) onCommand(_ mqtt.Client, m mqtt.Message) {
	// topic: aperture/<id|system>/<set|snapshot>/<record_mode|armed|trigger>
	parts := strings.Split(m.Topic(), "/")
	if len(parts) < 4 {
		return
	}
	payload := strings.TrimSpace(string(m.Payload()))
	switch {
	case parts[1] == "system" && parts[3] == "armed":
		p.setArmed(payload == "ON" || strings.EqualFold(payload, "true"))
	case parts[2] == "set" && parts[3] == "record_mode":
		p.setRecordModeCmd(parts[1], strings.ToLower(payload))
	case parts[2] == "snapshot" && parts[3] == "trigger":
		p.triggerSnapshot(parts[1])
	}
}

func (p *Publisher) setRecordModeCmd(cameraID, mode string) {
	if mode != store.ModeNever && mode != store.ModeContinuous && mode != store.ModeMotion {
		return
	}
	cam, err := p.st.GetCamera(cameraID)
	if err != nil {
		return
	}
	if cam.RecordMode != mode {
		cam.RecordMode = mode
		if err := p.st.UpdateCamera(cam); err != nil {
			p.log.Warn("ha: set record mode", "err", err)
			return
		}
		if p.reconcile != nil {
			p.reconcile()
		}
		p.log.Info("ha: record mode set from HA", "camera", cameraID, "mode", mode)
	}
	p.pub(stateTopicBase+"/"+cameraID+"/record_mode", true, mode)
}

func (p *Publisher) setArmed(armed bool) {
	_ = p.st.SetSetting("armed", boolStr(armed))
	p.log.Info("ha: armed set from HA", "armed", armed)
	p.pub(stateTopicBase+"/system/armed", true, onOff(armed))
	if p.onArmed != nil {
		p.onArmed(armed) // change feed alarm.state (Item 3)
	}
}

func (p *Publisher) triggerSnapshot(cameraID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if data, err := p.sm.SnapshotCached(ctx, cameraID); err == nil && len(data) > 0 {
		p.pub(stateTopicBase+"/"+cameraID+"/snapshot", true, data)
	}
}

// publishControlDiscovery announces the inbound controls: a record-mode Select
// per camera and a system Arm switch.
func (p *Publisher) publishControlDiscovery(pfx string, cams []*store.Camera) {
	for _, c := range cams {
		dev := camDevice(c.ID, c.Name)
		sel := map[string]any{
			"name":               "Record mode",
			"unique_id":          "aperture_" + c.ID + "_record_mode",
			"command_topic":      stateTopicBase + "/" + c.ID + "/set/record_mode",
			"state_topic":        stateTopicBase + "/" + c.ID + "/record_mode",
			"options":            []string{store.ModeNever, store.ModeMotion, store.ModeContinuous},
			"icon":               "mdi:record-rec",
			"availability_topic": statusTopic,
			"device":             dev,
		}
		p.pub(fmt.Sprintf("%s/select/aperture_%s_record_mode/config", pfx, c.ID), true, mustJSON(sel))
	}
	sw := map[string]any{
		"name":               "Armed",
		"unique_id":          "aperture_armed",
		"command_topic":      stateTopicBase + "/system/set/armed",
		"state_topic":        stateTopicBase + "/system/armed",
		"payload_on":         "ON",
		"payload_off":        "OFF",
		"icon":               "mdi:shield-home",
		"availability_topic": statusTopic,
		"device":             p.nvrDevice(),
	}
	p.pub(fmt.Sprintf("%s/switch/aperture_armed/config", pfx), true, mustJSON(sw))
}

// publishControlState publishes current per-camera record modes and armed state.
func (p *Publisher) publishControlState() {
	cams, err := p.st.ListCameras()
	if err != nil {
		return
	}
	for _, c := range cams {
		p.pub(stateTopicBase+"/"+c.ID+"/record_mode", true, c.RecordMode)
	}
	p.pub(stateTopicBase+"/system/armed", true, onOff(p.armed()))
}

// armed reports the current armed state (default armed unless explicitly off).
func (p *Publisher) armed() bool {
	v, _ := p.st.GetSetting("armed")
	return v != "false"
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
