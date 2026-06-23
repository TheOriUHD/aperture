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

## Running (Docker — recommended)

Only Docker is required; the image builds the web bundle and Go binaries itself.

```bash
make up           # docker compose up --build the full stack (nvrd + go2rtc + mosquitto)
```

Then open `http://<host>:8971` and create the admin account. The host port
defaults to **8971** — override it by copying `deploy/.env.example` to
`deploy/.env` and setting `APERTURE_PORT` (and `APERTURE_PUBLIC_URL`).

```bash
make down         # stop the stack
make logs         # follow logs
```

> `make build` compiles locally and therefore needs Go 1.25+ and Node 18+ on
> your PATH. You do **not** need it for the Docker workflow above.

## Local development

Requires Go 1.25+, Node 18+.

```bash
make web-dev      # vite dev server on :5173 (proxies /api to nvrd)
make run          # run nvrd on :8080 in another terminal
make build        # build the Go binaries + embed the web bundle
make test         # run Go + frontend tests
```

See `docs/` for setup, the Home Assistant integration, and the API reference.
The full build plan lives in the project plan file.
