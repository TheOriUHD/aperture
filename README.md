# Aperture

A self-hosted NVR (network video recorder) with a UniFi Protect-grade experience.

Ingests RTSP camera streams and records them — 24/7 with retention/overwrite,
scheduled time-plans, or motion-triggered clips — with classic motion-threshold
detection (no AI). Dark, dense, premium web UI for desktop and phone, role-based
access (admin / viewer), an admin TUI/CLI, and an outbound Home Assistant integration
(HA connects to Aperture to consume cameras and events).

## Architecture

```
 Cameras ──RTSP──▶ go2rtc ──┬─ RTSP restream ─▶ ffmpeg recorders (-c copy)
                            ├─ WebRTC/MSE ────▶ browser live view
                            └─ RTSP/WebRTC ───▶ Home Assistant
                     ▲
            nvrd (Go) configures go2rtc, supervises ffmpeg, owns SQLite,
            serves the React app + REST/WS API, publishes MQTT for HA.
```

- **Backend:** Go daemon `nvrd` (chi, SQLite via modernc.org/sqlite).
- **Streaming:** go2rtc (RTSP fan-out → WebRTC/MSE/HLS).
- **Recording:** ffmpeg segment muxer, stream-copy (no transcode).
- **Frontend:** React + TypeScript + Vite, bespoke UniFi-style design system.
- **CLI/TUI:** `nvrctl` (cobra + Bubble Tea).
- **Deploy:** Docker Compose (nvrd + go2rtc + mosquitto).

## Development

Requires Go 1.24+, Node 18+, Docker.

```bash
make dev          # run backend (nvrd) + frontend (vite) for local dev
make build        # build the Go binaries and the web bundle
make up           # docker compose up the full stack
make test         # run Go + frontend tests
```

See `docs/` for setup, the Home Assistant integration, and the API reference.
The full build plan lives in the project plan file.
