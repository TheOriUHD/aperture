#!/bin/sh
# Aperture container entrypoint. Before the daemon starts, harden the runtime data
# dir so a non-root backend-extension process (sharing this container's filesystem)
# cannot read Aperture's secrets or database even if it escapes the Deno sandbox
# (DD-FS). Runs as root; nvrd — also root — is unaffected. Then exec the daemon.
set -eu

mkdir -p /data/ext
# 0711 = traversable (a child can reach its own /data/ext/<id>) but NOT listable,
# so a child cannot enumerate siblings or the rest of /data.
chmod 0711 /data /data/ext 2>/dev/null || true

# Owner-only on the sensitive files. On a fresh boot the DB may not exist yet;
# store.Open re-applies 0600 to the DB files right after it creates them.
for f in secrets.json cli.token aperture.db aperture.db-wal aperture.db-shm go2rtc.yaml; do
  if [ -e "/data/$f" ]; then chmod 0600 "/data/$f" 2>/dev/null || true; fi
done

exec "$@"
