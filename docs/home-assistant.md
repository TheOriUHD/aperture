# Home Assistant integration

Aperture integrates with Home Assistant **outbound**: Aperture is the camera
source and publishes to an MQTT broker; Home Assistant connects to that broker
and auto-discovers everything. You never install Aperture inside Home Assistant.

## What you get

Per camera (as an HA device):
- **Motion** — `binary_sensor` (device class `motion`), ON while a motion event
  is active.
- **Recording** — `binary_sensor` (device class `running`).
- **Camera** — an MQTT camera entity showing the latest snapshot (refreshed
  ~every 10 s and on motion). For smooth **live video**, add the RTSP URL (below).

For the NVR (as an HA device):
- **Storage used** — sensor (GB).
- **Events today** — sensor.

## Setup

You need an MQTT broker. Use **either**:
- Home Assistant's **Mosquitto broker** add-on (recommended), or
- Aperture's built-in Mosquitto (the `mosquitto` service in the compose stack,
  reachable at `tcp://<aperture-host>:1883`).

### 1. Configure Aperture

Settings → **Home Assistant**:
- **MQTT broker URL** — e.g. `tcp://<broker-host>:1883`.
- **Username / Password** — your broker credentials (blank for anonymous).
- **Discovery prefix** — `homeassistant` (the HA default).
- Toggle **Enable MQTT publishing** and **Save**. The status badge turns
  **Connected** once it reaches the broker.

### 2. Configure Home Assistant

If HA's MQTT integration points at the **same broker**, Aperture's devices and
entities appear automatically under **Settings → Devices & Services → MQTT**.

If you used Aperture's built-in broker, add the MQTT integration in HA and point
it at `<aperture-host>:1883`.

### 3. Live video (optional)

The MQTT camera shows periodic snapshots. For real-time video, add each camera's
RTSP restream — shown (and copyable) in Aperture's Home Assistant settings:

```
rtsp://<aperture-host>:8554/cam_<id>
```

Add it in HA via the **go2rtc** integration (built in since 2024.11) or the
**Generic Camera** integration. These give a low-latency live feed in HA
dashboards.

## Example automation

```yaml
automation:
  - alias: "Light on front-door motion"
    trigger:
      - platform: state
        entity_id: binary_sensor.front_door_motion
        to: "on"
    action:
      - service: light.turn_on
        target:
          entity_id: light.porch
```

## Notes

- Snapshots are published retained, so HA shows the last frame immediately.
- Availability is published on `aperture/status` (online/offline via MQTT LWT);
  entities show as unavailable if Aperture goes down.
- Disable publishing any time from the same settings page.
