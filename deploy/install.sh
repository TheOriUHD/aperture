#!/usr/bin/env bash
# Install Aperture as native systemd services.
#
# Idempotent: safe to re-run for upgrades. Binaries, units and the SDK are
# replaced; /etc/aperture/aperture.env and everything under /var/lib/aperture are
# left alone — config and state must survive an upgrade.
#
# By default this installs and ENABLES the units but does not START them, because
# the first install on a migration target must not open camera streams before the
# database is in place. Pass --start for a fresh single-command install.
set -euo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(dirname "$SRC")"
BIN_DIR="${BIN_DIR:-$REPO/bin}"

DO_START=0
DO_VENDOR=1
FFMPEG_MODE="${FFMPEG_MODE:-pinned}"
for a in "$@"; do
  case "$a" in
    --start)        DO_START=1 ;;
    --skip-vendor)  DO_VENDOR=0 ;;
    --pin-ffmpeg)   FFMPEG_MODE=pinned ;;
    --system-ffmpeg) FFMPEG_MODE=system ;;
    -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
    *) echo "install: unknown option $a" >&2; exit 2 ;;
  esac
done

log()  { printf '\033[1m==>\033[0m %s\n' "$*"; }
info() { printf '    %s\n' "$*"; }
die()  { printf 'install: %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------- preflight
log "Preflight"
[ "$(id -u)" -eq 0 ] || die "must run as root"
command -v systemctl >/dev/null || die "systemd is required"
[ "$(uname -m)" = "x86_64" ] || die "only x86_64 is supported (got $(uname -m))"

# avahi owns 5353 exclusively and would break BOTH apdiscovery and the HomeKit
# bridge — and host-network HomeKit is a main reason for this install shape.
# Assert rather than purge: silently removing a package the operator installed
# for something else is not ours to do.
if command -v avahi-daemon >/dev/null || systemctl list-unit-files 2>/dev/null | grep -q '^avahi-daemon'; then
  die "avahi-daemon is present. It binds UDP/5353 exclusively and will break
  Aperture's mDNS advertisement and HomeKit pairing. Remove it, or disable and
  mask it (systemctl disable --now avahi-daemon avahi-daemon.socket), then re-run."
fi

# Ports must be free, but not "free because our own unit is stopped" — ignore
# ports held by an Aperture unit so re-runs and upgrades don't trip this.
if command -v ss >/dev/null; then
  for p in 8971 1883 1984 8554 8555; do
    holder=$(ss -lntupH "sport = :$p" 2>/dev/null | head -1 || true)
    [ -z "$holder" ] && continue
    case "$holder" in
      *nvrd*|*go2rtc*|*mosquitto*|*apdiscovery*) info "port $p held by an Aperture component (ok)" ;;
      *) die "port $p is already in use:
  $holder" ;;
    esac
  done
fi

for b in nvrd nvrctl apdiscovery; do
  [ -x "$BIN_DIR/$b" ] || die "$BIN_DIR/$b not found or not executable.
  Build first:  make build-native   (or set BIN_DIR=/path/to/binaries)"
done
info "binaries: $BIN_DIR"
info "ffmpeg mode: $FFMPEG_MODE"

# ---------------------------------------------------------------- packages
log "System packages"
PKGS="ca-certificates mosquitto"
[ "$FFMPEG_MODE" = system ] && PKGS="$PKGS ffmpeg"
if command -v apt-get >/dev/null; then
  missing=""
  for p in $PKGS; do dpkg -s "$p" >/dev/null 2>&1 || missing="$missing $p"; done
  if [ -n "$missing" ]; then
    info "installing:$missing"
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends $missing >/dev/null
  else
    info "already present: $PKGS"
  fi
else
  info "not a dpkg system — ensure these exist yourself: $PKGS"
fi

# ---------------------------------------------------------------- users/dirs
log "Users and directories"
install -m 0644 "$SRC/sysusers/aperture.conf"  /usr/lib/sysusers.d/aperture.conf
install -m 0644 "$SRC/tmpfiles/aperture.conf"  /usr/lib/tmpfiles.d/aperture.conf
systemd-sysusers >/dev/null
# systemd-sysusers exits 0 even when it refuses to create an account, so check
# the result rather than trusting the exit status. An absent aperture-ext means
# extension sandboxing has no base identity to fall back on.
for u in aperture-ext aperture-go2rtc; do
  getent passwd "$u" >/dev/null || die "systemd-sysusers did not create the '$u' account.
  Re-run manually to see why:  systemd-sysusers --cat-config | grep aperture"
done
[ "$(id -u aperture-ext)" = "65533" ] || die "aperture-ext exists but has uid $(id -u aperture-ext), expected 65533"
systemd-tmpfiles --create >/dev/null
info "aperture-ext ($(id -u aperture-ext)) and aperture-go2rtc ($(id -u aperture-go2rtc)) created"
info "/var/lib/aperture laid out"

# ---------------------------------------------------------------- runtime
if [ "$DO_VENDOR" -eq 1 ]; then
  log "Vendored runtime (pinned + sha256-verified)"
  FFMPEG_MODE="$FFMPEG_MODE" "$SRC/vendor.sh"
else
  log "Vendored runtime — skipped (--skip-vendor)"
fi

# ---------------------------------------------------------------- payload
log "Binaries, SDK and preflight script"
# 0755 is REQUIRED, not a default: the rlimit trampoline re-execs /proc/self/exe
# AFTER dropping to an extension uid, so nvrd must stay world-executable.
install -m 0755 "$BIN_DIR/nvrd"        /usr/bin/nvrd
install -m 0755 "$BIN_DIR/nvrctl"      /usr/bin/nvrctl
install -d -m 0755 /usr/lib/aperture/bin
install -m 0755 "$BIN_DIR/apdiscovery" /usr/lib/aperture/bin/apdiscovery
# The SDK gets a directory to itself: --allow-read grants the whole parent dir.
install -d -m 0755 /usr/lib/aperture/sdk
install -m 0444 "$SRC/aperture-sdk/mod.ts" /usr/lib/aperture/sdk/mod.ts
install -d -m 0755 /usr/libexec/aperture
install -m 0755 "$SRC/libexec/aperture-harden" /usr/libexec/aperture/aperture-harden

# ---------------------------------------------------------------- config
log "Configuration"
install -d -m 0755 /etc/aperture
if [ -e /etc/aperture/aperture.env ]; then
  info "/etc/aperture/aperture.env exists — left untouched"
  info "compare against $SRC/aperture.env.example after an upgrade"
else
  install -m 0640 "$SRC/aperture.env.example" /etc/aperture/aperture.env
  info "seeded /etc/aperture/aperture.env — SET APERTURE_STREAM_HOST AND"
  info "APERTURE_PUBLIC_URL before starting, or external stream URLs will be wrong"
fi
# go2rtc rewrites its own config and it holds camera RTSP passwords in cleartext.
# Seed only a credential-free base; a migration drops the real file in afterwards.
if [ ! -e /var/lib/aperture/go2rtc.yaml ]; then
  cat > /var/lib/aperture/go2rtc.yaml <<'YAML'
# Base go2rtc configuration. nvrd manages individual camera streams through the
# go2rtc API at runtime, and go2rtc persists them back into this file — which is
# why it ends up holding camera credentials and is chmod 0600.
api:
  listen: ":1984"
rtsp:
  listen: ":8554"
webrtc:
  listen: ":8555"
  candidates:
    - stun:8555
log:
  level: info
YAML
  info "seeded /var/lib/aperture/go2rtc.yaml"
fi
chown aperture-go2rtc:aperture-go2rtc /var/lib/aperture/go2rtc.yaml
chmod 0600 /var/lib/aperture/go2rtc.yaml

# ---------------------------------------------------------------- units
log "systemd units"
for u in aperture.service aperture-go2rtc.service aperture-discovery.service; do
  install -m 0644 "$SRC/systemd/$u" "/etc/systemd/system/$u"
  info "$u"
done
systemctl daemon-reload
systemctl enable aperture-go2rtc.service aperture-discovery.service aperture.service >/dev/null 2>&1
info "enabled (start on boot)"

if [ "$DO_START" -eq 1 ]; then
  log "Starting"
  systemctl start mosquitto.service 2>/dev/null || true
  systemctl start aperture-go2rtc.service
  systemctl start aperture.service
  systemctl start aperture-discovery.service
  sleep 2
  systemctl is-active --quiet aperture.service \
    && info "aperture.service is running" \
    || die "aperture.service failed to start — journalctl -u aperture -n 50"
fi

# ---------------------------------------------------------------- done
port=$(sed -nE 's/^APERTURE_HTTP_ADDR="?:?([0-9]+)"?.*/\1/p' /etc/aperture/aperture.env | tail -1)
log "Installed."
echo
if [ "$DO_START" -eq 1 ]; then
  info "Web UI:  http://$(hostname -I 2>/dev/null | awk '{print $1}'):${port:-8971}"
  info "Admin:   nvrctl"
else
  info "Units are enabled but NOT started (pass --start to start them)."
  info "Next:    edit /etc/aperture/aperture.env, then"
  info "         systemctl start aperture-go2rtc aperture aperture-discovery"
fi
info "Logs:    journalctl -u aperture -f"
