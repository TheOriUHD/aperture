#!/usr/bin/env bash
# Predict the retention janitor's first-pass decision BEFORE starting nvrd.
#
# Why this exists: on 2026-08-20 a layout in which the recordings tree sat inside
# the data dir made the janitor evict 21,953 segments seconds after start, with
# only a WARN line to show for it. This computes the same numbers the janitor
# will, so an eviction of the entire archive is caught while it is still a
# prediction rather than a deletion.
#
# Mirrors internal/retention/janitor.go:
#   apertureAppBytes = dirBytes(DataDir) + sum(dirBytes(rec/<cam>/events))
#   target           = storage_cap_bytes - apertureAppBytes   (default location)
#   evicts while       TotalBytesInLocation("") > target
set -euo pipefail

DB="${1:?usage: check-retention.sh <aperture.db> [data-dir] [recordings-dir]}"
DATA="${2:-/var/lib/aperture/data}"
REC="${3:-/var/lib/aperture/recordings}"

command -v sqlite3 >/dev/null || { echo "check-retention: sqlite3 required" >&2; exit 2; }

human() { numfmt --to=iec --suffix=B "$1" 2>/dev/null || echo "$1"; }

case "$REC/" in "$DATA"/*)
  echo "FAIL: recordings dir is inside the data dir — the janitor would count all"
  echo "      footage as app overhead and evict the entire archive."
  exit 1 ;;
esac

cap=$(sqlite3 "$DB" "SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM settings WHERE key='storage_cap_bytes'),0);")
if [ "$cap" -le 0 ]; then echo "PASS: no storage cap set (unlimited)"; exit 0; fi

app=$(du -sb "$DATA" 2>/dev/null | cut -f1); app=${app:-0}
ev=0
for d in "$REC"/*/events; do [ -d "$d" ] && ev=$(( ev + $(du -sb "$d" | cut -f1) )); done
app=$(( app + ev ))
target=$(( cap - app )); [ "$target" -lt 0 ] && target=0
total=$(sqlite3 "$DB" "SELECT COALESCE(SUM(size_bytes),0) FROM segments WHERE COALESCE(location_id,'')='';")

printf '  storage cap        %s\n' "$(human "$cap")"
printf '  app overhead       %s   (data dir + event thumbnails)\n' "$(human "$app")"
printf '  eviction target    %s\n' "$(human "$target")"
printf '  footage in default %s\n' "$(human "$total")"

if [ "$total" -gt "$target" ]; then
  over=$(( total - target ))
  pct=$(( total > 0 ? over * 100 / total : 0 ))
  echo "  -> the janitor WILL evict about $(human "$over") (~${pct}% of the archive)"
  [ "$pct" -ge 50 ] && { echo "FAIL: that is a mass deletion, not routine pruning."; exit 1; }
  echo "PASS: routine pruning only."
else
  echo "  -> no eviction on the first pass"
  echo "PASS"
fi
