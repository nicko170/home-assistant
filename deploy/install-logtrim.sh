#!/bin/zsh
# Install the hourly log trimmer on the mac mini (no sudo needed).
#   ssh 192.168.15.4 '~/ha/deploy/install-logtrim.sh'
# The live script goes to ~/ha-deploy/bin, outside the GitOps tree, the same
# way as mdns-reflect, so a bad commit cannot break it.
set -euo pipefail
REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR=/Users/nickp/ha-deploy/bin
PLIST_NAME=net.npratley.logtrim
PLIST="$HOME/Library/LaunchAgents/$PLIST_NAME.plist"

mkdir -p "$BIN_DIR"
install -m 755 "$REPO_DIR/deploy/logtrim.sh" "$BIN_DIR/logtrim.sh"
cp "$REPO_DIR/deploy/$PLIST_NAME.plist" "$PLIST"
launchctl bootout "gui/$(id -u)/$PLIST_NAME" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST"
launchctl list | grep "$PLIST_NAME"
