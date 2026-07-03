# Object detection (opt-in)

Aperture's object detection is **event-gated and opt-in**. Motion stays the
cheap, always-on trigger; only when an event opens does a detector classify a
short burst of frames and tag the event (`person`, `car`, `dog`…). The default
install ships **no detector** — detection mode is `off`, nothing runs, and the
daemon stays a pure-Go, CGO-free single binary.

Configure it under **Settings → Detection** (admin).

## Backends

### External server (recommended) — `bridge`

`nvrd` POSTs candidate frames to an inference server that speaks the
**DeepStack / CodeProject.AI** detection API (or a generic normalized-box JSON).
This keeps the daemon pure-Go and reuses hardware acceleration (GPU/Coral) on the
inference server. Point it at something you already run (Frigate's detector,
CodeProject.AI, DeepStack), or run a local sidecar (below).

Set **Detector URL** to e.g. `http://<host>:32168/v1/vision/detection`.

### Turnkey local sidecar (no cloud, no CGO)

"Local" doesn't require in-process inference — a detector container on the same
box is fully local. Add a service alongside `nvrd` in `deploy/docker-compose.yml`:

```yaml
  detector:
    image: codeproject/ai-server:cpu   # or a CUDA/Coral variant
    restart: unless-stopped
    ports:
      - "32168:32168"
    volumes:
      - cpai-data:/etc/codeproject/ai
# volumes:
#   cpai-data:
```

Then in **Settings → Detection** choose **External server** and set the URL to
`http://detector:32168/v1/vision/detection` (same compose network) or
`http://<host-ip>:32168/v1/vision/detection`. Click **Test** to confirm.

### Embedded in-process ONNX — deliberately not in the default image

An in-process model (via `onnxruntime_go`) is **CGO-only** and requires bundling
a native ONNX Runtime `.so`, which abandons the pure-Go/CGO-free build. To
preserve that build, embedded ONNX is intended to live behind a build tag /
separate `nvrd-ai` image rather than in the default binary. Selecting the
"Local model" backend without such a build reports unavailable and leaves
detection off (no crash).

## Tuning

- **Min confidence** — global floor when no per-label threshold is set.
- **Only these objects** — an allow-list; empty keeps every label. Each selected
  label can carry its own threshold.
- **Frames per event** — how many frames to sample per event (higher = better
  recall, more work).
- **Test** — runs the active backend on a live snapshot and shows what it sees.

## What detection drives

- **Alarms feed** shows the object label + bounding boxes on the thumbnail.
- **Notifications** titles read "Person · Front Door", and rules can gate on
  object labels (`conditions.objects`) so you notify on people, not leaves.
