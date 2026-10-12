# Third-party notices

Aperture's own source code is licensed under the [PolyForm Noncommercial License 1.0.0](LICENSE).
The components below are separate works under their own licenses, which are not changed by it.

## Libraries linked into the binaries

| Component | License | Used for |
|---|---|---|
| [`github.com/cloudsoda/sddl`](https://github.com/cloudsoda/sddl) | **LGPL-3.0** | Security-descriptor parsing for SMB network storage (via `go-smb2`) |
| [`github.com/hashicorp/go-uuid`](https://github.com/hashicorp/go-uuid), [`github.com/hashicorp/golang-lru`](https://github.com/hashicorp/golang-lru) | MPL-2.0 | Kerberos support for SMB; caching inside the SQLite driver |
| [`github.com/eclipse/paho.mqtt.golang`](https://github.com/eclipse/paho.mqtt.golang) | EPL-2.0 / EDL-1.0 | MQTT client for Home Assistant |
| Other Go modules | MIT, BSD-2/3-Clause, Apache-2.0, ISC | See `go.mod`; each module ships its own license file |

The LGPL-3.0 and MPL-2.0 libraries are used unmodified. Their complete source is available from
the links above, and the whole of Aperture is available in this repository, so a modified copy of
any of them can be built into your own binaries with `go build`.

## Web interface

| Component | License |
|---|---|
| [Inter](https://rsms.me/inter/) variable font (`@fontsource-variable/inter`) | SIL OFL 1.1 |
| `caniuse-lite` browser-support data (build time only) | CC BY 4.0 |
| Other npm packages | MIT, ISC, BSD, Apache-2.0 (see `web/package-lock.json`) |

## Runtime programs (not distributed here)

Aperture starts these as separate processes; they are installed by you or by `deploy/install.sh`
and keep their own licenses: [go2rtc](https://github.com/AlexxIT/go2rtc) (MIT) and
[FFmpeg](https://ffmpeg.org/) (LGPL/GPL depending on the build).

## Screenshots

The demo footage in the screenshots is freely licensed material from Wikimedia Commons; the
creators are credited in the README.
