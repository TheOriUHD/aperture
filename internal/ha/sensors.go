package ha

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Inbound sensor state (deferred #10): subscribe to Home Assistant sensor state
// topics (MQTT Statestream) so a camera's on-screen overlay can show a real
// thermometer reading from HA instead of only outdoor weather. Enable "MQTT
// Statestream" (with publish_attributes) in Home Assistant and point
// ha_sensor_topic at its sensor base path (default homeassistant/sensor).

// Sensor is one Home Assistant sensor reading cached from MQTT.
type Sensor struct {
	EntityID    string `json:"entityId"`
	Name        string `json:"name"`
	Value       string `json:"value"`
	Unit        string `json:"unit"`
	DeviceClass string `json:"deviceClass"`
	UpdatedAt   int64  `json:"updatedAt"`
}

func (s Sensor) display() string {
	if s.Name != "" {
		return s.Name
	}
	return s.EntityID
}

// isTemperature reports whether this reading is a thermometer — by device_class
// or a temperature unit — so only those are offered for a camera overlay.
func (s Sensor) isTemperature() bool {
	if strings.EqualFold(s.DeviceClass, "temperature") {
		return true
	}
	switch strings.ToUpper(strings.TrimSpace(s.Unit)) {
	case "°C", "°F", "℃", "℉", "C", "F", "K":
		return true
	}
	return false
}

// subscribeSensors wires the HA sensor state + attribute topics on (re)connect.
func (p *Publisher) subscribeSensors(c mqtt.Client) {
	base := p.sensorTopic()
	c.Subscribe(base+"/+/state", 0, p.onSensor)
	c.Subscribe(base+"/+/unit_of_measurement", 0, p.onSensor)
	c.Subscribe(base+"/+/friendly_name", 0, p.onSensor)
	c.Subscribe(base+"/+/device_class", 0, p.onSensor)
}

func (p *Publisher) onSensor(_ mqtt.Client, m mqtt.Message) {
	base := p.sensorTopic()
	rest, ok := strings.CutPrefix(m.Topic(), base+"/")
	if !ok {
		return
	}
	slash := strings.LastIndex(rest, "/")
	if slash < 0 {
		return
	}
	entity, leaf := rest[:slash], rest[slash+1:]
	val := mqttStr(m.Payload())
	p.sensorMu.Lock()
	if p.sensors == nil {
		p.sensors = map[string]*Sensor{}
	}
	s := p.sensors[entity]
	if s == nil {
		s = &Sensor{EntityID: entity}
		p.sensors[entity] = s
	}
	switch leaf {
	case "state":
		s.Value = val
		s.UpdatedAt = time.Now().Unix()
	case "unit_of_measurement":
		s.Unit = val
	case "friendly_name":
		s.Name = val
	case "device_class":
		s.DeviceClass = val
	}
	p.sensorMu.Unlock()
}

// Sensors returns cached TEMPERATURE readings with a numeric value, sorted by
// display name — the pool a camera can pick from for its overlay.
func (p *Publisher) Sensors() []Sensor {
	p.sensorMu.Lock()
	out := make([]Sensor, 0, len(p.sensors))
	for _, s := range p.sensors {
		if _, err := strconv.ParseFloat(s.Value, 64); err != nil {
			continue
		}
		if !s.isTemperature() {
			continue
		}
		out = append(out, *s)
	}
	p.sensorMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].display() < out[j].display() })
	return out
}

// Sensor returns a single cached reading by entity id (ok=false if unknown).
func (p *Publisher) Sensor(entityID string) (Sensor, bool) {
	p.sensorMu.Lock()
	defer p.sensorMu.Unlock()
	if s, ok := p.sensors[entityID]; ok {
		return *s, true
	}
	return Sensor{}, false
}

func (p *Publisher) sensorTopic() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current.SensorTopic != "" {
		return p.current.SensorTopic
	}
	return defaultSensorTopic
}

// mqttStr cleans a payload: MQTT Statestream publishes string attributes
// JSON-encoded (e.g. the unit arrives as `"°C"`), so decode those back to
// plain text; numeric states pass through untouched.
func mqttStr(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var out string
		if err := json.Unmarshal([]byte(s), &out); err == nil {
			return out
		}
	}
	return s
}
