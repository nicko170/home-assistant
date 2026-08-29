#!/bin/zsh
# Install the pf rules that let the LAN reach Home Assistant in the OrbStack VM.
#
# Run on the mini as root:
#     sudo ./deploy/install-pf-ha-container.sh
#
# Diagnoses first, installs, then proves the container did not lose LAN access.
# Safe to re-run. See deploy/pf-ha-container.conf for why this is needed.
#
# ROLLBACK, if anything goes sideways:
#     sudo pfctl -a ha-container -F all      # drop just these rules
#     sudo cp /etc/pf.conf.bak.<timestamp> /etc/pf.conf && sudo pfctl -f /etc/pf.conf

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  print -u2 "must run as root: sudo $0"
  exit 1
fi

REPO_DIR=${0:A:h:h}
ANCHOR=ha-container
CONF=/etc/pf.anchors/$ANCHOR
PFCONF=/etc/pf.conf
DOCKER=/usr/local/bin/docker

print "=== before ==="
print "  net.inet.ip.forwarding = $(sysctl -n net.inet.ip.forwarding)"
pfctl -s info 2>/dev/null | head -1 || true

mkdir -p /etc/pf.anchors
install -m 0644 -o root -g wheel "$REPO_DIR/deploy/pf-ha-container.conf" "$CONF"
print "installed $CONF"

# Wire the anchor into pf.conf, once. macOS rewrites pf.conf on some system
# updates, so this is written to be idempotent and re-runnable rather than a
# one-time hand edit. nat-anchor must precede the filter anchor: pf.conf is
# order-sensitive and a filter anchor first is silently ineffective for NAT.
if grep -q "anchor \"$ANCHOR\"" "$PFCONF"; then
  print "anchor already present in $PFCONF"
else
  cp "$PFCONF" "$PFCONF.bak.$(date +%Y%m%d-%H%M%S)"
  printf '\nnat-anchor "%s"\nanchor "%s"\nload anchor %s from "%s"\n' \
    "$ANCHOR" "$ANCHOR" "$ANCHOR" "$CONF" >> "$PFCONF"
  print "added anchor to $PFCONF (backup taken)"
fi

# -E enables pf and takes a reference count; harmless if already enabled.
pfctl -E 2>/dev/null || true
pfctl -f "$PFCONF"
print "pf reloaded"

print "=== anchor rules now loaded ==="
pfctl -a "$ANCHOR" -s nat 2>/dev/null || true
pfctl -a "$ANCHOR" -s rules 2>/dev/null || true

# The failure mode that matters: these rules must not cost the container its
# own outbound access. That regression is worse than the bug being fixed, so
# check it explicitly and self-revert rather than leaving a broken host.
print "=== verify: container must still reach the LAN ==="
if ! "$DOCKER" exec homeassistant python3 -c '
import socket
ok = 0
for h, p in [("192.168.15.82", 1400), ("192.168.15.22", 8009), ("192.168.15.240", 80)]:
    s = socket.socket(); s.settimeout(4)
    try:
        s.connect((h, p)); print("  OK   %s:%s" % (h, p)); ok += 1
    except Exception:
        print("  FAIL %s:%s" % (h, p))
    s.close()
raise SystemExit(0 if ok >= 2 else 1)
'; then
  print -u2 "container lost LAN access - flushing anchor and aborting"
  pfctl -a "$ANCHOR" -F all
  exit 1
fi

print
print "Installed. Now test from a LAN host that is NOT the mini:"
print "    nc -z 192.168.139.2 8123 && echo reachable"
print
print "Then watch for the Sonos subscription warning to stop:"
print "    docker logs -f homeassistant 2>&1 | grep -i sonos"
