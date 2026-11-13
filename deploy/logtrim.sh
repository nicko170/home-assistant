#!/bin/zsh
# Keep the mac mini's unbounded logs bounded. Runs hourly from
# net.npratley.logtrim (LaunchAgent); the live copy is in ~/ha-deploy/bin.
#
# Why this exists: nothing rotated these files. By 2026-11-14 Scrypted's logs
# were 2.3 GB, 196k RTMP reconnect lines from a two-week camera outage alone.
# They also held the Reolink admin password in cleartext: Scrypted's built-in
# RTMP client logs "Sending play command with path: ...password=...".
#
# The writers are launchd StandardOutPath/StandardErrorPath, which open with
# O_APPEND, so truncating in place is safe. They keep writing at the new end.
# When a file passes MAX_MB, keep its last KEEP_MB with secret-bearing lines
# removed. Files are also chmod 600, since they can hold credentials.
set -u
MAX_MB=${MAX_MB:-100}
KEEP_MB=${KEEP_MB:-10}
FILES=(
  /Users/nickp/.scrypted/scrypted.log
  /Users/nickp/.scrypted/scrypted.err.log
  /Users/nickp/ha-deploy/mdns-reflect.out.log
  /Users/nickp/ha-deploy/mdns-reflect.err.log
)
for f in $FILES; do
  [[ -f $f ]] || continue
  chmod 600 "$f"
  size=$(stat -f %z "$f")
  (( size > MAX_MB * 1048576 )) || continue
  tmp="$f.trim.$$"
  tail -c $(( KEEP_MB * 1048576 )) "$f" \
    | grep -av -e 'Sending play command' -e 'password=' > "$tmp"
  cat "$tmp" > "$f"
  rm -f "$tmp"
  print "$(date '+%F %T') trimmed $f from $(( size / 1048576 )) MB to ${KEEP_MB} MB"
done
