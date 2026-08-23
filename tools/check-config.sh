#!/usr/bin/env bash
# Validate the HA config locally, the same way the mac mini's deploy.sh does -
# by running check_config inside the real HA image - but before pushing.
#
# This is deliberately STRICTER than deploy.sh. Verified behaviour of
# check_config on 2026-08-23:
#
#   | Condition                   | check_config exit | deploy.sh action |
#   |-----------------------------|-------------------|------------------|
#   | YAML/parse error            | 1                 | rolls back       |
#   | Config SCHEMA error         | 0                 | DEPLOYS ANYWAY   |
#   | Reference to missing entity | 0                 | deploys anyway   |
#
# The middle row is the dangerous one: a typo'd option ships and silently
# disables that component. We catch it by grepping for "Invalid config for".
# The bottom row cannot be caught here at all - entity references must be
# verified against the live system. See the spec's Testing section.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG="$REPO/homeassistant/config"
VERSION="$(tr -d '[:space:]' < "$CONFIG/.HA_VERSION")"
IMAGE="ghcr.io/home-assistant/home-assistant:${VERSION}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Runtime state would confuse check_config, and is gitignored anyway.
rsync -a --exclude '.storage/' --exclude 'backups/' --exclude 'deps/' \
         --exclude '*.db*' --exclude '*.log*' --exclude '.cache/' \
         "$CONFIG/" "$WORK/config/"

# secrets.yaml is gitignored, so synthesise one covering every !secret key.
# Values must still satisfy each option's schema: trusted_proxies wants an IP,
# so key names that look like hosts get an IP rather than a bare string.
grep -rhoE '!secret[[:space:]]+[A-Za-z0-9_]+' "$WORK/config" \
  | awk '{print $2}' | sort -u | while read -r key; do
      case "$key" in
        *host*|*ip*|*addr*|*proxy*) echo "$key: 10.0.0.1" ;;
        *)                          echo "$key: placeholder" ;;
      esac
    done > "$WORK/config/secrets.yaml"

echo "Validating $CONFIG with $IMAGE"
set +e
OUT="$(docker run --rm -v "$WORK/config:/config" "$IMAGE" \
        python -m homeassistant --script check_config -c /config 2>&1)"
RC=$?
set -e

echo "$OUT"

if [ "$RC" -ne 0 ]; then
  echo >&2 "FAIL: check_config exited $RC (YAML parse error)"
  exit 1
fi

if echo "$OUT" | grep -q "Invalid config for"; then
  echo >&2 "FAIL: schema error - deploy.sh would NOT catch this"
  exit 1
fi

# ---------------------------------------------------------------------------
# Stage 2: dashboards.
#
# check_config does NOT parse YAML-mode dashboards - verified 2026-08-23 by
# pointing family.yaml at a nonexistent !include and watching it exit 0 in
# silence. HA loads those files lazily, when a browser first requests them, so
# a broken include surfaces as a blank dashboard at the wall rather than as a
# failed deploy. Parse them explicitly with HA's own loader, which resolves
# !include exactly as HA does at request time.
# ---------------------------------------------------------------------------
echo "Validating dashboards"
set +e
DOUT="$(docker run --rm -v "$WORK/config:/config" "$IMAGE" python -c '
import glob, sys
from homeassistant.util.yaml import load_yaml
ok = True
for f in ["/config/ui-lovelace.yaml"] + sorted(glob.glob("/config/dashboards/*.yaml")):
    try:
        load_yaml(f)
        print("  ok   " + f)
    except Exception as e:
        ok = False
        print("  FAIL " + f + ": " + str(e))
sys.exit(0 if ok else 1)
' 2>&1)"
DRC=$?
set -e
echo "$DOUT"
if [ "$DRC" -ne 0 ]; then
  echo >&2 "FAIL: a dashboard file does not parse - it would render blank on the wall"
  exit 1
fi

echo "OK: config is valid"
