package ha

import (
	"fmt"
	"strconv"
)

// Presence entities: per camera with object recognition, whether a person,
// vehicle or animal is there (occupancy sensors), how many people, whether a
// vehicle stands parked, and, on cameras watching for packages, whether a
// delivered parcel waits. Unlike motion they stay on while someone sits still:
// recognition keeps looking at the published cameras every second or so while
// Home Assistant is connected.

type presenceEntity struct {
	key       string // topic and unique-id suffix
	component string // binary_sensor | sensor
	name      string
	class     string // device class ("" for none)
	icon      string
}

var presenceEntities = []presenceEntity{
	{"person", "binary_sensor", "Person", "occupancy", "mdi:account"},
	{"people", "sensor", "People", "", "mdi:account-group"},
	{"vehicle", "binary_sensor", "Vehicle", "occupancy", "mdi:car"},
	{"animal", "binary_sensor", "Animal", "occupancy", "mdi:paw"},
	{"parked", "binary_sensor", "Parked vehicle", "occupancy", "mdi:parking"},
	{"package", "binary_sensor", "Package", "", "mdi:package-variant-closed"},
}

// publishPresenceDiscovery announces a camera's presence entities, or takes
// them away when recognition doesn't look at the camera (or, for Package,
// when it doesn't watch for packages).
func (p *Publisher) publishPresenceDiscovery(pfx, cameraID string, dev map[string]any) {
	var pr Presence
	ok := false
	if p.presence != nil {
		pr, ok = p.presence(cameraID)
	}
	for _, e := range presenceEntities {
		topic := fmt.Sprintf("%s/%s/aperture_%s_%s/config", pfx, e.component, cameraID, e.key)
		if !ok || (e.key == "package" && !pr.Packages) {
			p.pub(topic, true, "") // retained: an empty config removes the entity
			continue
		}
		cfg := map[string]any{
			"name":               e.name,
			"unique_id":          "aperture_" + cameraID + "_" + e.key,
			"state_topic":        stateTopicBase + "/" + cameraID + "/" + e.key,
			"icon":               e.icon,
			"availability_topic": statusTopic,
			"device":             dev,
		}
		if e.class != "" {
			cfg["device_class"] = e.class
		}
		if e.component == "binary_sensor" {
			cfg["payload_on"], cfg["payload_off"] = "ON", "OFF"
		} else {
			cfg["state_class"] = "measurement"
		}
		p.pub(topic, true, mustJSON(cfg))
	}
}

// publishPresence publishes each camera's presence when it changes.
func (p *Publisher) publishPresence() {
	if p.presence == nil {
		return
	}
	cams, err := p.st.ListCameras()
	if err != nil {
		return
	}
	for _, c := range haCameras(cams) {
		pr, ok := p.presence(c.ID)
		if !ok {
			pr = Presence{} // recognition stopped: nothing is seen
		}
		p.stateMu.Lock()
		prev, seen := p.present[c.ID]
		p.present[c.ID] = pr
		p.stateMu.Unlock()
		if seen && prev == pr {
			continue
		}
		base := stateTopicBase + "/" + c.ID + "/"
		send := func(key string, changed bool, v string) {
			if !seen || changed {
				p.pub(base+key, true, v)
			}
		}
		send("person", prev.Person != pr.Person, onOff(pr.Person))
		send("people", prev.People != pr.People, strconv.Itoa(pr.People))
		send("vehicle", prev.Vehicle != pr.Vehicle, onOff(pr.Vehicle))
		send("animal", prev.Animal != pr.Animal, onOff(pr.Animal))
		send("parked", prev.Parked != pr.Parked, onOff(pr.Parked))
		send("package", prev.Package != pr.Package, onOff(pr.Package))
		if seen && prev.Packages != pr.Packages {
			// A camera started or stopped watching for packages: its Package
			// entity comes or goes.
			p.publishPresenceDiscovery(p.prefix(), c.ID, camDevice(c.ID, c.Name))
		}
	}
}

// publishedCameraIDs are the enabled cameras Home Assistant gets.
func (p *Publisher) publishedCameraIDs() map[string]bool {
	cams, err := p.st.ListCameras()
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, c := range haCameras(cams) {
		if c.Enabled {
			out[c.ID] = true
		}
	}
	return out
}

// keepLooking asks recognition to follow these cameras for presence (nil:
// none, Home Assistant isn't connected).
func (p *Publisher) keepLooking(ids map[string]bool) {
	if p.keepPresence == nil || (len(ids) == 0 && !p.keptAll) {
		return
	}
	p.keepPresence(ids)
	p.keptAll = len(ids) > 0
}
