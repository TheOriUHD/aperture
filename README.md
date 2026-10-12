<div align="center">

<img src="docs/media/icon.webp" width="96" height="96" alt="Aperture">

# Aperture

**A self-hosted camera recorder that feels like a product, not a project.**<br>
Record your IP cameras around the clock, find any moment in seconds and watch live from any device,<br>
on your own hardware, with no cloud and no subscription.

[![Release](https://img.shields.io/github/v/tag/TheOriUHD/aperture?label=release&style=flat-square&labelColor=0b0e14&color=1d9bf0)](https://github.com/TheOriUHD/aperture/tags)
[![License: PolyForm Noncommercial](https://img.shields.io/badge/license-PolyForm%20Noncommercial-1d9bf0?style=flat-square&labelColor=0b0e14)](LICENSE)
[![Platform](https://img.shields.io/badge/runs%20on-Linux%20x86--64-1d9bf0?style=flat-square&labelColor=0b0e14&logo=linux&logoColor=white)](#requirements)
[![Self-hosted](https://img.shields.io/badge/self--hosted-no%20cloud-34d399?style=flat-square&labelColor=0b0e14)](#what-aperture-wont-do)
[![Home Assistant](https://img.shields.io/badge/Home%20Assistant-ready-41bdf5?style=flat-square&labelColor=0b0e14&logo=homeassistant&logoColor=white)](#smart-home)

**[Features](#features)** &nbsp;·&nbsp; **[Requirements](#requirements)** &nbsp;·&nbsp; **[Cameras](#cameras)** &nbsp;·&nbsp; **[Install](#install)** &nbsp;·&nbsp; **[First setup](#first-setup)** &nbsp;·&nbsp; **[Advanced](#advanced)** &nbsp;·&nbsp; **[FAQ](#faq)**

<br>

<img src="docs/media/live-wall.gif" width="880" alt="The live wall: nine cameras covering one site, all streaming, with weather, storage and bitrate on the right">

<sub>Nine cameras at one site, live. Everything in this README comes from a real Aperture install; the footage is credited at the bottom.</sub>

</div>

<br>

## What is Aperture?

Aperture turns a spare PC, a mini PC or a virtual machine into the recorder for your IP cameras. You give it each camera's address, and it records everything to your own disks, lets you watch live and scrub back through history in a fast web app (also on your phone), and tells you when something happens.

- **Nothing in the middle.** Footage never leaves your hardware unless you export or share it. No account, no telemetry, nothing phones home.
- **Recording that doesn't degrade your video.** Aperture stores exactly what the camera sends, with no re-compression, so quality is untouched and the load on your machine stays low.
- **Works with the cameras you already own.** Anything that can produce an RTSP stream, which is most IP cameras. See the [camera list](#cameras).
- **Plays well with the rest of your home.** Home Assistant, HomeKit and ONVIF are built in; the API is open and versioned.

> **Honest scope.** Aperture is a single-site recorder for homes, small offices and workshops. It is not a multi-site VMS, and it does not do cloud relay: you reach it on your network, or through a VPN or reverse proxy you control. See [What Aperture won't do](#what-aperture-wont-do).

<br>

## Features

### Watch live

<img src="docs/media/live.webp" alt="The dashboard: nine cameras in a three-by-three wall with recording badges and the current weather on each tile; system health, storage and bitrate on the right" width="100%">

- **Every camera at once.** A live wall of all cameras, or just one group ("Downstairs", "Outside").
- **Fast and low latency.** Streams are kept warm in the background, so a camera you open is usually already running. Live video uses WebRTC, with an automatic fallback for browsers or networks that block it.
- **Self-healing.** A camera drops, your phone locks and unlocks, the Wi-Fi hiccups: the picture comes back on its own. No black tile that needs a page reload.
- **Sub-streams for the grid.** If your camera offers a second, smaller stream, Aperture uses it for the wall and switches to full quality when you open a single camera.
- **Context at a glance.** Recording state per camera, the weather at the site, storage used, live bitrate and the latest detections.

### Scrub through history

<table>
<tr>
<td width="60%" valign="top"><img src="docs/media/playback-scrub.gif" alt="Scrubbing the playback timeline for one camera: the picture follows the timeline as it is dragged"></td>
<td valign="top">

A vertical timeline under your finger or cursor that **scrubs smoothly through days of footage**, with recorded coverage, events and the live edge in a single rail.

- Click an event in the strip below the player and playback opens at that moment.
- See the **weather at that moment**, reconstructed from history.
- Zoom the picture, jump 10 seconds back or forward, take a snapshot.
- Multi-camera layouts for following something across views.
- A built-in list of keyboard shortcuts (the keyboard button at the bottom left of the player).

</td>
</tr>
</table>

<img src="docs/media/playback.webp" alt="Playback of the main lobby camera with the timeline on the left and a strip of detected events along the bottom" width="100%">

### Know what happened

<img src="docs/media/alarms.webp" alt="The alarms page: a grid of events across all cameras, each with a thumbnail, detected objects outlined, the camera name, time and weather" width="100%">

- **Motion detection on every camera**, with a sensitivity you set per camera and **zones** you can paint over to ignore the parts of the picture that always move (a tree, a road).
- **People, vehicles and animals**, if you add an object detector (optional; see *Object detection* under [Advanced](#advanced)). It only looks at the few frames motion already flagged, so it costs nothing while nothing happens.
- **Audio detection** for cameras with a microphone, if you want it.
- **Filter by conditions.** Every event is stamped with the weather and time of day it happened in, so you can show only the ones from the rain, the night, the day or a windy spell.
- **Arm and disarm** like an alarm panel, and build simple rules: when something happens, notify, call a webhook, publish to MQTT or change how a camera records.
- **Notifications that respect you.** Web push to your phone with a thumbnail, plus ntfy, Telegram, Discord, email and signed webhooks, with quiet hours and per-camera rules.

### On your phone

<img src="docs/media/mobile.webp" alt="Aperture on a phone: the live view with detection counters, playback with a touch timeline, and the alarms feed" width="92%">

A **dedicated mobile layout**, not a shrunken desktop page: live view with detection counters, playback with a touch-friendly timeline, and the alarm feed. It installs to your home screen like an app and supports push notifications.

> Phones only allow installing, push notifications and passkeys on secure (HTTPS) pages. Put Aperture behind a reverse proxy with a certificate; see [Reach it from your phone](#reach-it-from-your-phone).

### Your storage, your rules

<img src="docs/media/system.webp" alt="System health: every camera stream with its state, live streaming status, storage and maintenance buttons for backup, diagnostics and restore" width="100%">

- **Record to the local disk, a second disk mounted under the data folder, a NAS share (SMB) or an SFTP server.** A guided setup wizard (**Settings → System → Storage**) tests the connection step by step and tells you plainly what failed.
- **Mirror to several places at once.** The same camera can record to the local disk (for instant playback) and to the NAS, each with its own retention. If one destination is briefly unreachable, recording continues and the mirror catches up by itself.
- **Per-camera retention and quotas.** Keep the front door for 30 days and a spare indoor camera for 3.
- **Space you reserve stays reserved.** Aperture can claim its share of a disk up front so other software cannot fill it and stop your recording. When storage reaches its limit, the oldest footage is removed first; recording never stops because a disk filled up.
- **Credentials are encrypted** wherever Aperture stores them, including your NAS password.
- **Export and share.** Save a time range as a normal video file, combine several cameras into one, make a timelapse, or create a link that gives a neighbour or an insurer one clip and nothing else.

### Weather, sun and moon

<img src="docs/media/environment.webp" alt="Location and weather settings with a map pin, coordinates, time zone and units" width="100%">

Set where the site is and Aperture records the weather alongside your footage, so an event from three months ago still tells you it was 4 °C and raining. That explains a smeared lens, a false trigger from a swaying branch or why night vision looks the way it does. It also shows sunrise, sunset and the moon phase. Weather is optional and uses the free [Open-Meteo](https://open-meteo.com) service; without internet, everything else keeps working and the weather parts simply stay hidden.

### Smart home

<img src="docs/media/integrations.webp" alt="The Home Assistant integration settings: MQTT broker, discovery prefix, and the RTSP restream address for each camera" width="100%">

- **Home Assistant**, with nothing to configure by hand. Cameras, motion sensors, recording state and storage appear automatically over MQTT discovery. Add each camera's RTSP restream to Home Assistant for smooth live video. See [docs/home-assistant.md](docs/home-assistant.md).
- **HomeKit.** Publish your cameras to Apple Home.
- **ONVIF.** Let Frigate, Blue Iris, Synology Surveillance Station, Milestone and similar systems discover and use Aperture's cameras. See [What ONVIF does](#what-onvif-does-for-you).
- **A plain RTSP restream** of every camera for any software on your network that wants one.

### Accounts and security

<table>
<tr>
<td width="38%" valign="top"><img src="docs/media/twofactor.webp" alt="Two-factor sign-in with a Trust this device option"></td>
<td width="62%" valign="top"><img src="docs/media/security.webp" alt="Security settings: two-factor authentication on, passkeys, signed-in devices and users and roles"></td>
</tr>
</table>

- **Passkeys, authenticator-app codes and single sign-on (OIDC).** A device that has passed the code once is remembered, so signing in there again takes only your password. You stay signed in for months on devices you use.
- **Every signed-in device is listed** and can be signed out in one click.
- **Roles and per-camera access.** Administrators manage the system; everyone else just watches, and you can limit each person to the cameras you choose. A guest or a lodger sees exactly what you intend.
- **Camera passwords never leave the server.** Links are shown with the login masked; leave the dots to keep it, type over them to change it.
- **A tamper-evident activity log** of sign-ins, setting changes, deletions and exports, hash-chained so tampering shows.

<table>
<tr>
<td width="50%" valign="top"><img src="docs/media/camera-editor.webp" alt="The camera editor with the camera's login masked in its address"></td>
<td width="50%" valign="top"><img src="docs/media/activity.webp" alt="The activity log listing sign-ins, camera changes and two-factor events"></td>
</tr>
</table>

### Make it yours with extensions

<img src="docs/media/extensions.webp" alt="The extensions page listing an installed extension that is active" width="100%">

Add panels and background automations without waiting for a release: room lights from the camera view, pan/tilt controls, a webhook on motion. Each extension is **sandboxed and only gets what you approve at install time**: which cameras it may see, whether it can reach the internet and where. It cannot read your recordings, your database or your passwords, and a misbehaving one cannot stop your cameras from recording. Four examples are in [`examples/`](examples); the guide is [docs/extensions](docs/extensions/README.md).

### Built to keep recording

- A **watchdog** restarts any camera stream that stalls and logs that it happened.
- The recording path is treated as the thing that must never fail: a crash, a full disk, a broken extension or a flaky camera is designed not to stop the others.
- **Backup and restore** of your configuration (cameras, users, rules, settings) so rebuilding on new hardware is not a from-scratch job. Footage is deliberately not part of a backup.
- A **diagnostics bundle** in one click, repair tools (rebuild the footage index) and an **admin console in the terminal** (`nvrctl`) for when the web app is not convenient.
- English and German interface, following your device's language by default.

<br>

## What Aperture won't do

- **No cloud, no account, no subscription, no paywalled features.** Everything described here is included.
- **No telemetry.** The only outside calls are the optional ones you turn on: weather (Open-Meteo), the location search and map tiles (Open-Meteo, OpenStreetMap), and push notifications through your browser vendor's push service.
- **No built-in HTTPS.** Aperture speaks plain HTTP on your network; use a reverse proxy for TLS.
- **No automatic camera discovery.** You add cameras by their RTSP address (it takes a minute; the app tests the connection and shows a live preview before you save).
- **No built-in object detector.** Detection is opt-in and uses a small inference server you run next to it.
- **x86-64 Linux only** for now. There is no packaged installer yet; you build it from source with one command.
- **One site, one server.** There is no clustering or multi-site management.

<br>

## Requirements

| You need | What that means |
|---|---|
| **A server** | Any 64-bit **x86 Linux** machine with systemd: a mini PC, an old laptop or a NUC. A Raspberry Pi or other ARM board is *not* supported. **Debian 13** is what Aperture is developed and run on; Debian and Ubuntu are the easy path because the installer uses `apt`. A virtual machine (for example on Proxmox) is fine. |
| **Enough CPU and RAM** | Recording copies the camera's stream without re-encoding, so it is light. Motion detection decodes each camera's picture, and that is where the CPU goes. As a reference, the author's install records **four cameras on a VM with 4 vCPUs and 4 GB of RAM**. |
| **Storage** | A disk for footage, sized to how much history you want (see below). A 24/7 recording workload suits a surveillance-class drive (such as WD Purple or Seagate SkyHawk). The operating system can live on a small SSD. |
| **Cameras that speak RTSP** | See the [camera list](#cameras). Wired (PoE) cameras are the most reliable. Battery cameras usually do not offer a continuous stream. |
| **A network** | Server and cameras on the same LAN (or routable to each other). Wired Ethernet for the server and, ideally, the cameras. Give the server and the cameras **fixed addresses** (a DHCP reservation in your router is enough), so they do not move. |
| **A modern browser** | Chrome, Edge, Firefox or Safari on desktop; Safari or Chrome on phones. |
| **Optional extras** | A reverse proxy for HTTPS (needed for phone install, push and passkeys), an MQTT broker or Home Assistant, a NAS, and an object-detection server. |

### How much storage?

Footage size depends on your cameras' **bitrate** (set in the camera's own settings; H.265 is usually about half of H.264 for the same quality):

| Camera bitrate | Per camera, per day | 4 cameras, 14 days |
|---|---|---|
| 2 Mbit/s (720p to 1080p, modest) | about 22 GB | about 1.2 TB |
| 4 Mbit/s (typical 1080p to 4 MP) | about 43 GB | about 2.4 TB |
| 6 Mbit/s (4 to 5 MP) | about 65 GB | about 3.6 TB |
| 8 Mbit/s (4K H.264) | about 86 GB | about 4.8 TB |

Motion-only recording uses a fraction of that. The app shows what is used, by which camera, and roughly how far back your footage reaches.

<br>

## Cameras

Aperture needs only one thing from a camera: an **RTSP** stream. **ONVIF** is a bonus that unlocks a few extras. Neither needs the manufacturer's cloud.

### RTSP and ONVIF in plain words

- **RTSP** is the standard way a camera hands out its live video over your network. You give Aperture an address like `rtsp://user:password@192.168.1.21/stream1`, and it can record and show that camera. Nearly every IP camera speaks it, though some hide it behind a setting.
- **ONVIF** is a separate industry standard that lets cameras and recorders discover and control each other: stream profiles, pan/tilt/zoom, picture settings and events. A camera can have RTSP without ONVIF. Aperture records fine either way.

### Camera compatibility

A sampling of popular cameras, **not a certified list**. Support varies by model and firmware, so look for "RTSP" and "ONVIF" in your camera's manual or web interface.

✅ supported &nbsp;·&nbsp; ⚠️ only on some models or with a catch &nbsp;·&nbsp; ❌ not available

| Brand / family | RTSP | ONVIF | Notes |
|---|:-:|:-:|---|
| **Reolink** (RLC / RLN PoE, Duo, E1 Pro) | ✅ | ✅ | Turn on the RTSP and ONVIF ports in the camera's network settings (often off by default). Streams: `/Preview_01_main` and `/Preview_01_sub`. Battery models need a Reolink Home Hub. **Extra in Aperture:** spotlight and IR illuminator controls. |
| **TP-Link Tapo** (mains-powered C-series) | ✅ | ✅ | Create a **Camera Account** in the Tapo app first and use it in the address. Streams: `/stream1` (high quality) and `/stream2` (standard). ONVIF Profile S on port 2020. **Extra:** pan/tilt control through the Tapo example extension. |
| **Hikvision** and its rebrands (Annke, Hilook and others) | ✅ | ✅ | Streams: `/Streaming/Channels/101` (main) and `/102` (sub). ONVIF has to be enabled and uses its own user. |
| **Dahua** and its rebrands (Amcrest, Imou) | ✅ | ✅ | Streams: `/cam/realmonitor?channel=1&subtype=0` (main) and `subtype=1` (sub). |
| **Axis** | ✅ | ✅ | Stream: `/axis-media/media.amp`, with options in the query string. Commercial-grade, long firmware support. |
| **Hanwha Vision** (Wisenet) | ✅ | ✅ | Commercial-grade; both are standard. |
| **TP-Link VIGI** | ✅ | ✅ | The business line from the Tapo maker. |
| **UniFi Protect cameras** | ⚠️ | ❌ | RTSP(S) must be switched on per camera in the Protect app, which needs a UniFi console. They do not speak ONVIF. |
| **Eufy** | ⚠️ | ⚠️ | Only some indoor and wired SoloCam models offer RTSP (enable it in the Eufy app); battery cameras do not. ONVIF is partial or absent. Pan/tilt works through a bridge: see the Eufy example extension. |
| **EZVIZ** | ⚠️ | ⚠️ | Many models offer RTSP with the device's verification code as the password; ONVIF only on some. |
| **Wyze** | ⚠️ | ❌ | RTSP only on certain models with special firmware; newer models are inconsistent. |
| **Ring, Google Nest, Blink, most Arlo** | ❌ | ❌ | Cloud-only. They do not expose a local stream. |
| **DIY** (Raspberry Pi camera, an old phone) | ✅ | ❌ | Run a small RTSP server such as MediaMTX on the board; ONVIF is usually not available. |

> Most brands also have a **second, lower-resolution stream** ("sub stream"). Give Aperture both: it uses the small one for the live wall and for motion detection, and the full-quality one for recording and when you open a single camera.

### What ONVIF does for you

ONVIF works in two directions, and Aperture uses both in a small, careful way.

| | What happens | What you get | Turned on by |
|---|---|---|---|
| **Aperture → your camera** | Aperture asks the camera's ONVIF service for its make, model and firmware, and for its picture settings. | Adjust **brightness, contrast, saturation, sharpness and day/night (IR cut)** from the camera editor, without opening the vendor's web page. Aperture also learns whether the camera has pan/tilt. | The **Camera picture settings** switch on a camera (off by default: nothing contacts the camera over ONVIF until you turn it on) |
| **Your camera → Aperture** | Not used. Video travels over RTSP, and Aperture does its own motion detection from the picture, so it does not depend on the camera's motion events. | Works the same with or without ONVIF. | Always |
| **Aperture → other recorders** | Aperture can present itself as an ONVIF device (Profile S) with a stream and snapshot per camera. | Frigate, Blue Iris, Synology Surveillance Station, Milestone and similar systems can **discover and consume** your cameras through Aperture. | **Settings → Integrations → Interop** (off by default) |

> The ONVIF provider is for trusted networks only. When enabled it answers requests on your LAN without a login, so keep it off unless you need it.

<br>

## Install

Aperture runs as three small systemd services on one machine. The instructions below assume a fresh **Debian 13** (or Ubuntu) server and take about ten minutes. There is no packaged release yet; `make install` builds everything on the machine itself and installs it.

### 1. Prepare the server

Install the basic tools (as a normal user with `sudo`):

```bash
sudo apt update
sudo apt install -y git make curl unzip nodejs npm
```

Install **Go 1.25 or newer**. Debian's own package may be older, so use the official one: download the `linux-amd64` tarball from [go.dev/dl](https://go.dev/dl/) into your home folder, then unpack it:

```bash
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.25.*.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile && . ~/.profile
go version
```

Two things on the server would stop Aperture from starting. Check them now:

```bash
# Avahi uses the mDNS port Aperture needs. This should print "inactive" or "could not be found":
systemctl is-active avahi-daemon

# If it is running, turn it off:
sudo systemctl disable --now avahi-daemon avahi-daemon.socket
```

Aperture also uses these ports, so nothing else should be listening on them: **8971** (web app), **1883** (MQTT), **1984**, **8554** and **8555** (video).

### 2. Get the code and install

```bash
git clone https://github.com/TheOriUHD/aperture.git
cd aperture
make install
```

`make install` builds the web app and the program, then runs the installer, which asks for your `sudo` password. It:

- installs **ffmpeg** and **mosquitto** from your distribution and creates the service users and folders;
- downloads the pinned **go2rtc** (video broker) and **Deno** (extension runtime) and verifies their checksums before installing;
- installs and enables three services: `aperture-go2rtc`, `aperture` and `aperture-discovery`. It does **not** start them yet.

### 3. Tell Aperture its own address

Open the configuration file:

```bash
sudo nano /etc/aperture/aperture.env
```

Set these two lines. They are what other devices use to reach Aperture:

```bash
# The server's address on your network (find it with: hostname -I)
APERTURE_STREAM_HOST="192.168.1.10"

# How you will open Aperture in a browser. Plain http on your LAN is fine to start with.
APERTURE_PUBLIC_URL="http://192.168.1.10:8971"
```

Save the file (<kbd>Ctrl</kbd>+<kbd>O</kbd>, <kbd>Enter</kbd>, <kbd>Ctrl</kbd>+<kbd>X</kbd>).

### 4. Start it

```bash
sudo systemctl start mosquitto aperture-go2rtc aperture aperture-discovery
systemctl status aperture --no-pager
```

You should see `active (running)`. If not, see [Troubleshooting](#troubleshooting).

### 5. Open it

Go to **`http://<server-address>:8971`** in a browser, for example `http://192.168.1.10:8971`. On networks where mDNS works, `http://aperture.local:8971` is also advertised.

<br>

## First setup

1. **Create the administrator.** Aperture asks for an email address, a name and a password (at least 8 characters). This only appears once, on a fresh install.
2. **Add a camera.** Open **Settings → Cameras → Add camera**:
   - Give it a name ("Front door").
   - Paste its **RTSP address** into *Main stream*, and the low-resolution one into *Sub stream* if it has one. See [Cameras](#cameras) for the usual address of your brand.
   - Click **Test connection**. Aperture connects and shows a live preview, so you know it works before you save.
   - Choose how it records (**24/7 continuous**, or motion only) and how many days to keep.
3. **Repeat for each camera.** They appear on the dashboard within seconds.
4. **Set where you live** under **Settings → Environment** (search an address or drop the pin) to get weather, sunrise and sunset in your footage.
5. **Optional, but worth doing now:**
   - **Storage:** point footage at a big disk or a NAS under **Settings → System → Storage**. See [Where footage goes](#where-footage-goes).
   - **Notifications:** install the app on your phone and switch push on; see [Reach it from your phone](#reach-it-from-your-phone).
   - **Two-factor:** turn it on under **Settings → Security**.

### Where footage goes

By default, footage and the database live in `/var/lib/aperture` (`recordings/` and `data/`). To use a bigger disk, **mount it at `/var/lib/aperture/recordings`**: the service is sandboxed and can only write below `/var/lib/aperture`. For a NAS, add an **SMB or SFTP destination** in the app instead; a wizard walks you through it.

> Never put the recordings folder *inside* the data folder. Aperture would count the footage as part of its own database and could delete recordings to "make room".

### Reach it from your phone

Phones only let a website install itself, send push notifications and use passkeys over **HTTPS**. The simplest way is a reverse proxy in front of Aperture. With [Caddy](https://caddyserver.com), which handles certificates for you, the whole configuration is:

```caddyfile
nvr.example.com {
    reverse_proxy 127.0.0.1:8971
}
```

Then change `APERTURE_PUBLIC_URL` to `https://nvr.example.com` and restart Aperture. Keep `APERTURE_STREAM_HOST` as the server's LAN address: the RTSP restream for Home Assistant and other players does not travel through the proxy.

If you only need access from outside, a VPN (WireGuard, Tailscale) is the safest option: nothing is exposed to the internet.

### Update, restart and uninstall

```bash
# Update: pull, rebuild, reinstall (your settings and footage are untouched), then restart
cd ~/aperture && git pull
make install
sudo systemctl restart aperture-go2rtc aperture

# Logs
journalctl -u aperture -f

# Uninstall (keeps your configuration and recordings)
sudo deploy/uninstall.sh
# ...and to also delete them:  sudo deploy/uninstall.sh --purge-state --purge-config
```

### Troubleshooting

<details>
<summary><b>The installer stops with "port is already in use" or mentions avahi</b></summary>
<br>

The installer checks ports 8971, 1883, 1984, 8554 and 8555 and refuses to continue if something else holds one. It names the program. Stop or move that program and run `make install` again. If it names `avahi-daemon`, disable it as shown in [Prepare the server](#1-prepare-the-server): avahi and Aperture both need the mDNS port and cannot share it.

</details>

<details>
<summary><b>Camera tiles stay black, or "Test connection" fails</b></summary>
<br>

Open the same address in VLC (<i>Media → Open Network Stream</i>) from another computer. If VLC cannot play it either, the problem is the camera: RTSP is switched off, the account or path is wrong, or the password contains special characters (some cameras mishandle them; use letters and digits). If VLC plays it, check that the server can reach the camera: `ping <camera address>`.

</details>

<details>
<summary><b>Live video works on the server's address but not through my reverse proxy</b></summary>
<br>

The web app and the live video travel through the proxy, but the raw RTSP restream does not. Make sure `APERTURE_STREAM_HOST` is the server's LAN address (not the proxy name), and that your proxy forwards WebSocket connections (Caddy and recent nginx do by default).

</details>

<details>
<summary><b>The picture is black for a camera that sends H.265</b></summary>
<br>

Aperture records H.265 (HEVC) fine, but whether a browser can show it live varies by browser and device. If a camera has a setting for H.264, use that for the sub stream, which is the one the live wall uses.

</details>

<details>
<summary><b>It records, but footage vanishes sooner than expected</b></summary>
<br>

Retention is set per camera (7 days by default), and storage has an allocation: when it is reached, the oldest footage goes first. Check the camera's retention and the **Storage** card in **Settings → System**.

</details>

<br>

## Advanced

<details>
<summary><b>Architecture</b></summary>
<br>

```mermaid
flowchart LR
    cams["IP cameras<br/>(RTSP)"] -->|one connection each| g2r["go2rtc<br/>stream broker"]
    g2r -->|RTSP restream| rec["Recorder<br/>ffmpeg -c copy<br/>10 s segments"]
    g2r -->|RTSP restream| det["Motion · audio ·<br/>optional objects"]
    g2r -->|WebRTC / MSE| ui["Browser & PWA"]
    g2r -->|RTSP| ha["Home Assistant"]
    rec --> disk[("Storage<br/>local · SMB · SFTP")]
    nvrd["nvrd (Go)<br/>API · web app · SQLite"] -.->|configures| g2r
    nvrd -.->|supervises| rec
    nvrd --- ui
    nvrd -->|MQTT discovery| ha
```

- **One ingress per camera.** go2rtc holds the only connection to each camera and fans it out to the recorder, the detectors, browsers and Home Assistant, so a camera sees one client whatever is watching.
- **`nvrd`** is a single Go daemon: the REST and WebSocket API, the embedded React app, the recorder supervisor, detection, retention and the extension supervisor, all over one SQLite database.
- **Deployment** is native systemd units: `aperture`, `aperture-go2rtc` and `aperture-discovery` (mDNS, so `aperture.local` works).

| | |
|---|---|
| Backend | Go 1.25, chi, SQLite (modernc, no cgo) |
| Streaming | go2rtc: WebRTC, MSE, HLS, RTSP restream |
| Recording | ffmpeg segment muxer, stream copy |
| Frontend | React 18, TypeScript, Vite, TanStack Query, Tailwind |
| Extensions | Deno, one sandbox per extension |

</details>

<details>
<summary><b>Configuration reference</b></summary>
<br>

Only settings that must exist before the database opens live in `/etc/aperture/aperture.env`; everything else is configured in the app. Changes need `sudo systemctl restart aperture`.

| Variable | Default (installed) | Purpose |
|---|---|---|
| `APERTURE_HTTP_ADDR` | `:8971` | Listen address of the web app and API |
| `APERTURE_PUBLIC_URL` | `http://localhost:8971` | How browsers reach Aperture; used to build links and stream URLs |
| `APERTURE_STREAM_HOST` | *(empty)* | LAN address external players (VLC, Home Assistant) use for the RTSP restream |
| `APERTURE_DATA_DIR` | `/var/lib/aperture/data` | Database, keys, caches |
| `APERTURE_RECORDINGS_DIR` | `/var/lib/aperture/recordings` | Recorded segments. Must not be inside the data directory |
| `APERTURE_GO2RTC_URL` / `APERTURE_GO2RTC_RTSP` | `http://127.0.0.1:1984` / `rtsp://127.0.0.1:8554` | Where Aperture finds go2rtc |
| `APERTURE_MQTT_URL` | `tcp://127.0.0.1:1883` | Broker for the Home Assistant integration; empty disables it |
| `APERTURE_HOMEKIT_ENABLED` / `APERTURE_ONVIF_ENABLED` | off | Force the HomeKit bridge or the ONVIF provider on (also in **Settings → Integrations → Interop**) |
| `APERTURE_JWT_SECRET` / `APERTURE_STREAM_SECRET` | generated | Signing keys. Left unset, Aperture generates and persists them under the data directory |
| `APERTURE_ADVERTISE_NAME` / `_HOST` / `_PORT` | `Aperture NVR` / `aperture` / `8971` | The mDNS name `aperture-discovery` announces |

</details>

<details>
<summary><b>Object detection</b></summary>
<br>

Motion is the cheap, always-on trigger. Only when an event opens does a detector classify a short burst of frames and tag the event (`person`, `car`, `dog` and so on). Aperture ships **no detector** by default, so the daemon stays a single static binary.

Run an inference server that speaks the **DeepStack / CodeProject.AI** detection API next to Aperture (for example the CodeProject.AI container, on a CPU, GPU or Coral), then set **Settings → Detection** to *External server* and enter its address, such as `http://<host>:32168/v1/vision/detection`. **Test** runs it on a live snapshot. You can restrict it to chosen labels, set a confidence per label and choose how many frames to sample per event. Details are in [docs/detection.md](docs/detection.md).

</details>

<details>
<summary><b>Storage destinations and mirrors</b></summary>
<br>

A *destination* is a place footage is written: the default local folder, another local folder (under `/var/lib/aperture`), an SMB share or an SFTP server. Each camera points at one destination for new footage, and can additionally **mirror** to others, each with its own retention. Footage stays watchable as long as any copy survives; a destination that is briefly unreachable is caught up automatically. Credentials are encrypted at rest. Space can be **hard-reserved** per destination so nothing else can consume it.

</details>

<details>
<summary><b>Home Assistant, MQTT, HomeKit and ONVIF</b></summary>
<br>

- **MQTT discovery:** per camera, a motion sensor, a recording sensor and a snapshot camera entity; for the NVR, storage used and events today. Point Aperture at your broker in **Settings → Integrations → Home Assistant**. Full guide: [docs/home-assistant.md](docs/home-assistant.md).
- **Live video in Home Assistant:** add each camera's RTSP restream (`rtsp://<server>:8554/cam_<id>`, shown and copyable in the app) through the go2rtc or Generic Camera integration.
- **HomeKit:** switch the bridge on in **Settings → Integrations → Interop**; the pairing code is generated per install and shown there. It needs host networking and port 5353 free, which is why the installer refuses to run next to avahi.
- **ONVIF provider:** Profile S, device and media services with optional WS-Discovery, off by default and unauthenticated by design, for trusted networks only.

</details>

<details>
<summary><b>API, tokens and automation</b></summary>
<br>

Everything the web app does goes through a **versioned, frozen REST API** (`/api/v1`) with an OpenAPI description generated from the route table (`GET /api/v1/openapi.json`), a change feed, ETags and idempotency keys. Create **scoped API tokens** (`apt_…`) in the app, limited to the actions and cameras a script needs. Webhooks from rules are HMAC-signed.

</details>

<details>
<summary><b>Extensions</b></summary>
<br>

An extension is a bundle with a manifest, optional UI panels and an optional Deno backend. The manifest declares exactly what it needs (cameras, events, network hosts, storage); you approve that in plain language when installing, and you can re-scope the cameras later without re-installing. Backends run as an unprivileged per-extension user with resource limits. Install from a file or a link, update (with fresh approval if new permissions are wanted), roll back, view logs and edit files in the app. The examples in [`examples/`](examples) cover pan/tilt control for Tapo and Eufy cameras, Home Assistant room lights and a motion webhook. Guide: [docs/extensions](docs/extensions/README.md).

</details>

<details>
<summary><b>The terminal console: nvrctl</b></summary>
<br>

`nvrctl` with no arguments opens a full-screen admin console over SSH (sign in, deep configuration). Subcommands give quick, scriptable read-outs using the local CLI token: `nvrctl status`, `nvrctl cameras`, `nvrctl users`, `nvrctl storage` and `nvrctl events`.

</details>

<details>
<summary><b>Security model</b></summary>
<br>

- The daemon runs under a **sandboxed systemd unit**: read-only system, a single writable state directory, no new privileges, restricted kernel and namespace access.
- go2rtc runs as its own user; its API listens on loopback only, because it holds camera passwords. Browsers go through Aperture's authenticated proxy.
- Camera credentials and destination secrets are **encrypted at rest**; sessions are stored server-side and only a hash of the cookie token is kept.
- Extensions are separated by user, by directory and by an explicit network allow-list.
- The activity log is **hash-chained**; the app can verify the chain.

</details>

<details>
<summary><b>Development</b></summary>
<br>

```bash
make run        # the daemon on :8080 (one terminal)
make web-dev    # the Vite dev server on :5173, proxying /api (another terminal)
make test       # Go and frontend tests
make build      # web bundle embedded into the Go binaries
make lint       # go vet
```

```
cmd/nvrd          the daemon            internal/api       HTTP API and access control
cmd/nvrctl        admin TUI and CLI     internal/record    recorder, indexer, mirrors
cmd/apdiscovery   mDNS advertiser       internal/stream    go2rtc orchestration
web/              React app             internal/store     SQLite and migrations
deploy/           systemd, install      internal/ext       extension supervisor
```

The Go code needs Linux (it uses Linux-only calls); develop in a Linux VM or container. [CHANGELOG.md](CHANGELOG.md) lists what changed in each release.

</details>

<br>

## FAQ

<details>
<summary><b>Do I need an internet connection?</b></summary>
<br>

No. Recording, live view, playback and alerts on your network all work offline. Only the optional weather, map search and web push notifications use the internet.

</details>

<details>
<summary><b>Will my cameras still phone home to their manufacturer?</b></summary>
<br>

Aperture does not make them. Whether the camera itself talks to its vendor's cloud is a setting on the camera: if you want it fully local, block the camera's internet access in your router (a separate VLAN for cameras is the classic way).

</details>

<details>
<summary><b>Can I record only when something moves?</b></summary>
<br>

Yes, per camera. You can also record on a schedule (only overnight, only when you are out). Motion clips keep some seconds before and after the trigger, so you see the whole approach.

</details>

<details>
<summary><b>Can I try it without buying cameras?</b></summary>
<br>

Yes. Any RTSP source works, including a video file looped through a small RTSP server (go2rtc and MediaMTX can both do this). That is how the demo in this README was made.

</details>

<details>
<summary><b>Is it open source? Can I use it at work?</b></summary>
<br>

The source is public and you can use, modify and redistribute it for any **noncommercial** purpose under the [PolyForm Noncommercial License 1.0.0](LICENSE). That covers personal, hobby, research and educational use and charities. Commercial use, including use inside a business, needs a separate license; get in touch through the repository. Because of that restriction it is "source-available" rather than open source in the OSI sense.

</details>

<br>

## License

Aperture is licensed under the **[PolyForm Noncommercial License 1.0.0](LICENSE)**: free for personal and other noncommercial use, with attribution (keep the `Required Notice` line in `LICENSE` when you redistribute). Third-party components keep their own licenses; see [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md).

## Credits

**Footage.** The screenshots come from a demo install fed with 75-second loops cut from the **"Multiview Extended Video with Activities" (MEVA) dataset by Kitware, Inc. and the Intelligence Advanced Research Projects Activity (IARPA)**, licensed under [Creative Commons Attribution 4.0](https://creativecommons.org/licenses/by/4.0/). The clips were cropped, scaled and re-encoded, and the object boxes, labels, weather and camera names shown are Aperture's own. MEVA was recorded with consenting actors at a controlled site and is available at [mevadata.org](https://mevadata.org). Object detections in the demo come from a local [YOLOX](https://github.com/Megvii-BaseDetection/YOLOX) model (Apache-2.0) behind Aperture's detection interface. The map pin and weather in the screenshots are a demo setting; they do not describe where the footage was filmed.

**Data and services.** Weather and place search by [Open-Meteo](https://open-meteo.com) (CC BY 4.0). Map data © [OpenStreetMap](https://www.openstreetmap.org/copyright) contributors. Streaming by [go2rtc](https://github.com/AlexxIT/go2rtc), recording with [FFmpeg](https://ffmpeg.org). Interface typeface: [Inter](https://rsms.me/inter/).
