#!/bin/zsh
# Build and install mdns-reflect on the mac mini.
#
# Run this on the mini, from a checkout of this repo. Safe to re-run: it is how
# you upgrade after changing the Go source.
#
# The binary is installed OUTSIDE the repo, matching deploy.sh - the deployer
# hard-resets the working tree on a bad commit, and a running service should
# not be collateral damage.

set -euo pipefail

REPO_DIR=${0:A:h:h}
SRC="$REPO_DIR/tools/mdns-reflect"
BIN_DIR=/Users/nickp/ha-deploy/bin
PLIST_NAME=net.npratley.mdns-reflect
PLIST="$HOME/Library/LaunchAgents/$PLIST_NAME.plist"
GO=${GO:-/usr/local/go/bin/go}

[[ -x "$GO" ]] || { print -u2 "go not found at $GO (set GO=...)"; exit 1 }

print "building from $SRC"
mkdir -p "$BIN_DIR"
( cd "$SRC" && "$GO" test ./... && "$GO" build -o "$BIN_DIR/mdns-reflect" . )

print "installing LaunchAgent"
cp "$REPO_DIR/deploy/$PLIST_NAME.plist" "$PLIST"

# bootout is idempotent-ish: it fails when not loaded, which is fine on a first
# install, so it must not trip the errexit above.
launchctl bootout "gui/$(id -u)/$PLIST_NAME" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"

# bootstrap returns before launchd has necessarily registered the job, so poll
# rather than sleeping once - a single short sleep reports a false failure on a
# service that started fine.
for _ in {1..10}; do
  if launchctl list | grep -q "$PLIST_NAME"; then
    print "running:"
    launchctl list | grep "$PLIST_NAME"
    exit 0
  fi
  sleep 1
done

print -u2 "FAILED to start; check /Users/nickp/ha-deploy/mdns-reflect.err.log"
exit 1
