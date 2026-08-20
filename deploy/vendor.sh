#!/usr/bin/env bash
# Fetch Aperture's pinned runtime dependencies into /usr/lib/aperture/bin.
#
# Everything is pinned by VERSION and verified by SHA-256. A reproducible runtime is
# what makes "installable with one command" mean the same thing on every machine — and
# it is the only way to be sure the recorder behaves as it does in production.
#
# The deno and go2rtc pins below were verified against the binaries actually running in
# production: deno is byte-identical, and go2rtc reports the same version AND the same
# git commit (b5948cf) — the size differs only because the GitHub release is UPX-packed
# while the container ships it unpacked.
set -euo pipefail

DEST="${DEST:-/usr/lib/aperture/bin}"
FFMPEG_MODE="${FFMPEG_MODE:-system}"   # system | pinned

DENO_VERSION="2.9.5"
DENO_URL="https://github.com/denoland/deno/releases/download/v${DENO_VERSION}/deno-x86_64-unknown-linux-gnu.zip"
DENO_SHA="dc480c462c8c3582524f3e75c160613d0a975e1f66b5465995d58bae236da7d3"   # the extracted binary

GO2RTC_VERSION="1.9.14"
GO2RTC_URL="https://github.com/AlexxIT/go2rtc/releases/download/v${GO2RTC_VERSION}/go2rtc_linux_amd64"
GO2RTC_SHA="32d616af226bd731678ffde328b94cfb94e30339bfefc469cfb76323144615a6"

# ffmpeg comes from the DISTRO by default. The migration originally pinned a
# static 5.1.1 build for parity with production 5.1.9 — and that pin was wrong:
#
#   johnvansickle 5.1.1-static SIGSEGVs (exit 139) probing our MPEG-TS segments.
#
# The recorder probes every finished segment with `ffprobe -show_entries
# format=duration` (internal/record/recorder.go:661). A crashing ffprobe returns
# no duration, so EVERY segment is discarded as "unreadable, skipping": files
# accumulate on disk, nothing is registered in the database, the timeline stops
# advancing and the watchdog restarts streams in a loop. Recording looks healthy
# from the outside the whole time. Debian's 7.1.5 reads the identical files fine.
#
# So the parity argument lost to a simpler one: the pinned build has to actually
# work, and a distro package gets security updates — which matters for a component
# parsing RTSP from cameras. The cost is that Debian's ffmpeg pulls ~180 packages
# onto a headless box.
#
# If you do pin, VALIDATE THE PIN: probe a real segment and check the exit code.
# "It reports the right --version" proves nothing, which is exactly how this got
# through review.
FFMPEG_URL="${FFMPEG_URL:-https://johnvansickle.com/ffmpeg/old-releases/ffmpeg-5.1.1-amd64-static.tar.xz}"
FFMPEG_SHA="${FFMPEG_SHA:-2d848c37f7f276be0262d9123a84fbfe72bb159921ffcce3bd7106f143d29f42}"

log() { printf '  %s\n' "$*"; }
die() { printf 'vendor: %s\n' "$*" >&2; exit 1; }

verify() { # file expected_sha label
  local got; got=$(sha256sum "$1" | cut -d' ' -f1)
  [ "$got" = "$2" ] || die "$3 checksum mismatch
  expected $2
  got      $got
  Refusing to install an unverified binary."
  log "$3 sha256 verified"
}

command -v curl >/dev/null || die "curl is required"
install -d -m 0755 "$DEST"
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT

log "fetching deno ${DENO_VERSION}"
curl -fsSL -o "$tmp/deno.zip" "$DENO_URL"
if command -v unzip >/dev/null; then
  unzip -q -o "$tmp/deno.zip" -d "$tmp"
else
  python3 -c "import zipfile,sys; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])" "$tmp/deno.zip" "$tmp"
fi
verify "$tmp/deno" "$DENO_SHA" "deno"
# 0755: extension children run as uid 65600+ and must be able to execute this.
install -m 0755 "$tmp/deno" "$DEST/deno"

log "fetching go2rtc ${GO2RTC_VERSION}"
curl -fsSL -o "$tmp/go2rtc" "$GO2RTC_URL"
verify "$tmp/go2rtc" "$GO2RTC_SHA" "go2rtc"
install -m 0755 "$tmp/go2rtc" "$DEST/go2rtc"

case "$FFMPEG_MODE" in
  system)
    # Use the distro package: signed, maintained, and security-patched — which matters
    # for a component parsing RTSP from cameras. Validate it in the rehearsal before
    # cutover (segment continuity + durations), since Debian 13 ships 7.x while
    # production is validated on 5.1.9.
    command -v ffmpeg  >/dev/null || die "FFMPEG_MODE=system but ffmpeg is not installed (apt install ffmpeg)"
    command -v ffprobe >/dev/null || die "FFMPEG_MODE=system but ffprobe is not installed"
    log "ffmpeg: using system $(ffmpeg -version 2>/dev/null | head -1 | awk '{print $3}') (not vendored)"
    ;;
  pinned)
    [ -n "$FFMPEG_URL" ] && [ -n "$FFMPEG_SHA" ] || die "FFMPEG_MODE=pinned requires FFMPEG_URL and FFMPEG_SHA"
    log "fetching pinned ffmpeg"
    curl -fsSL -o "$tmp/ff.tar.xz" "$FFMPEG_URL"
    verify "$tmp/ff.tar.xz" "$FFMPEG_SHA" "ffmpeg"
    tar -xJf "$tmp/ff.tar.xz" -C "$tmp"
    for b in ffmpeg ffprobe; do
      f=$(find "$tmp" -type f -name "$b" -perm -u+x | head -1)
      [ -n "$f" ] || die "$b not found in the pinned archive"
      install -m 0755 "$f" "$DEST/$b"
    done
    log "ffmpeg: pinned build installed to $DEST"
    ;;
  *) die "FFMPEG_MODE must be 'system' or 'pinned'" ;;
esac

# --- ffmpeg smoke test ------------------------------------------------------
# Verify the chosen ffmpeg/ffprobe can actually do the ONE thing the recorder
# needs: segment to MPEG-TS, then report a duration. The pinned 5.1.1 build
# passed --version and SIGSEGV'd here, which cost a cutover.
ffmpeg_check() {
  local FF FP t
  FF=$( [ -x "$DEST/ffmpeg" ]  && echo "$DEST/ffmpeg"  || command -v ffmpeg )
  FP=$( [ -x "$DEST/ffprobe" ] && echo "$DEST/ffprobe" || command -v ffprobe )
  t=$(mktemp -d)
  set +e
  "$FF" -v error -f lavfi -i testsrc=size=128x72:rate=10:duration=2 \
        -c:v libx264 -f mpegts "$t/probe.ts" </dev/null >/dev/null 2>&1
  local erc=$?
  set -e
  [ $erc -eq 0 ] || { rm -rf "$t"; die "$FF exited $erc producing an MPEG-TS segment (139 = SIGSEGV)."; }
  local dur rc
  # errexit off deliberately: a SIGSEGV here must produce the diagnostic below,
  # not kill the script at the assignment with a bare exit 139 and no explanation.
  set +e
  dur=$("$FP" -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$t/probe.ts" 2>/dev/null); rc=$?
  set -e
  rm -rf "$t"
  [ $rc -eq 0 ] || die "$FP exited $rc probing an MPEG-TS segment (139 = SIGSEGV).
  The recorder probes every finished segment this way; a crashing ffprobe makes it
  discard all of them as 'unreadable' while files pile up untracked on disk."
  case "$dur" in ''|0|N/A) die "$FP returned no duration for an MPEG-TS segment ('$dur').
  Every recorded segment would be dropped as unreadable." ;; esac
  log "ffmpeg smoke test passed (probed ${dur}s from a generated MPEG-TS)"
}
ffmpeg_check

log "vendored runtime in $DEST:"
for b in deno go2rtc ffmpeg ffprobe; do
  [ -e "$DEST/$b" ] && printf '    %-8s %s\n' "$b" "$($DEST/$b --version 2>&1 | head -1 | cut -c1-46)"
done
