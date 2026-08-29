#!/bin/zsh
# Enable IPv4 forwarding on the mac mini, now and at every boot.
#
# Needs root, which is the only reason this is a separate script rather than
# part of install-mdns-reflect.sh. Run it on the mini:
#
#     sudo ./deploy/install-ip-forward.sh
#
# See deploy/net.npratley.ip-forward.plist for why this is required.

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  print -u2 "must run as root: sudo $0"
  exit 1
fi

REPO_DIR=${0:A:h:h}
PLIST_NAME=net.npratley.ip-forward
DEST=/Library/LaunchDaemons/$PLIST_NAME.plist

install -m 0644 -o root -g wheel "$REPO_DIR/deploy/$PLIST_NAME.plist" "$DEST"

launchctl bootout system/$PLIST_NAME 2>/dev/null || true
launchctl bootstrap system "$DEST"

# Apply immediately too - bootstrap runs the job, but be explicit so the
# script's own output proves the live state rather than assuming it.
sysctl -w net.inet.ip.forwarding=1 >/dev/null

print "net.inet.ip.forwarding = $(sysctl -n net.inet.ip.forwarding)"
[[ "$(sysctl -n net.inet.ip.forwarding)" == "1" ]] || { print -u2 "FAILED to enable"; exit 1 }
print "installed $DEST (persists across reboot)"
