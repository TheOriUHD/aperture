#!/usr/bin/env bash
# Remove Aperture's native install.
#
# State is NEVER touched by default: /var/lib/aperture holds the database, the
# encryption key and every recording, and /etc/aperture holds the config. Both
# survive unless you explicitly ask for them to be destroyed.
set -euo pipefail

PURGE_STATE=0
PURGE_CONFIG=0
ASSUME_YES=0
for a in "$@"; do
  case "$a" in
    --purge-state)  PURGE_STATE=1 ;;
    --purge-config) PURGE_CONFIG=1 ;;
    --yes|-y)       ASSUME_YES=1 ;;
    -h|--help) sed -n '2,8p' "$0"; exit 0 ;;
    *) echo "uninstall: unknown option $a" >&2; exit 2 ;;
  esac
done

log()  { printf '\033[1m==>\033[0m %s\n' "$*"; }
info() { printf '    %s\n' "$*"; }
die()  { printf 'uninstall: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "must run as root"

log "Stopping units"
for u in aperture-discovery.service aperture.service aperture-go2rtc.service; do
  systemctl disable --now "$u" >/dev/null 2>&1 || true
  info "$u stopped and disabled"
done

log "Removing installed files"
rm -f  /etc/systemd/system/aperture.service \
       /etc/systemd/system/aperture-go2rtc.service \
       /etc/systemd/system/aperture-discovery.service
rm -f  /usr/bin/nvrd /usr/bin/nvrctl
rm -rf /usr/lib/aperture /usr/libexec/aperture
rm -f  /usr/lib/tmpfiles.d/aperture.conf /usr/lib/sysusers.d/aperture.conf
systemctl daemon-reload
info "units, binaries, vendored runtime and SDK removed"

# The sysusers entries are deliberately left in place: files under
# /var/lib/aperture/ext are owned by those uids, and removing the users while
# their files remain leaves unowned uids on disk.
info "aperture-ext / aperture-go2rtc users kept (they still own files on disk)"

if [ "$PURGE_CONFIG" -eq 1 ]; then
  rm -rf /etc/aperture
  info "/etc/aperture removed"
else
  info "/etc/aperture kept (--purge-config to remove)"
fi

if [ "$PURGE_STATE" -eq 1 ]; then
  size=$(du -sh /var/lib/aperture 2>/dev/null | cut -f1)
  if [ "$ASSUME_YES" -ne 1 ]; then
    printf '\n  This deletes /var/lib/aperture (%s) — the database, the encryption\n' "${size:-unknown}"
    printf '  key and ALL recordings. Network-storage credentials become unreadable\n'
    printf '  without the key, so remote footage is lost too. This cannot be undone.\n\n'
    printf '  Type DESTROY to confirm: '
    read -r reply
    [ "$reply" = "DESTROY" ] || die "aborted — nothing deleted"
  fi
  rm -rf /var/lib/aperture
  info "/var/lib/aperture removed"
else
  info "/var/lib/aperture kept — database and recordings intact (--purge-state to remove)"
fi

log "Uninstalled."
