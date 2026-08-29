#!/bin/zsh
# Install the pf rules that let the LAN reach Home Assistant in the OrbStack VM.
#
# Run on the mini as root:
#     sudo ./deploy/install-pf-ha-container.sh
#
# Safe to re-run: it strips its own previous edits before re-applying, so a
# failed run does not compound. See deploy/pf-ha-container.conf for the why.
#
# ROLLBACK:
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

cp "$PFCONF" "$PFCONF.bak.$(date +%Y%m%d-%H%M%S)"

# pf.conf is strictly ordered: options, normalization, queueing, TRANSLATION,
# then FILTERING. Appending a nat-anchor at the end of a file that already has
# Apple's filter anchors is a syntax error, not a warning - pf refuses the
# whole ruleset. So each anchor has to be spliced into its own section rather
# than tacked on the end.
ANCHOR="$ANCHOR" PFCONF="$PFCONF" CONF="$CONF" python3 <<'PYEOF'
import os, re

anchor = os.environ["ANCHOR"]
pfconf = os.environ["PFCONF"]
conf = os.environ["CONF"]

lines = open(pfconf).read().splitlines()

# Strip any previous attempt so re-runs are idempotent rather than cumulative.
# A failed run leaves its lines behind; without this they compound.
lines = [l for l in lines if anchor not in l]

nat_line = 'nat-anchor "%s"' % anchor
flt_line = 'anchor "%s"' % anchor
load_line = 'load anchor "%s" from "%s"' % (anchor, conf)

def first_filter(ls):
    return next((n for n, l in enumerate(ls)
                 if re.match(r'anchor\b', l.strip())), len(ls))

def last_filter(ls):
    hits = [n for n, l in enumerate(ls) if re.match(r'anchor\b', l.strip())]
    return hits[-1] if hits else None

# pf.conf is strictly ordered and translation must precede filtering. Appending
# a nat-anchor at the end of a file that already has Apple's filter anchors is
# what broke the first attempt - pf rejects the whole ruleset, it does not warn.
#
# Insert immediately BEFORE the first filter anchor. That is the one position
# guaranteed to be after every earlier section without having to model what
# those sections are; anchoring off the last nat/rdr line instead is fragile,
# because Apple's own file puts dummynet-anchor after them.
lines.insert(first_filter(lines), nat_line)

j = last_filter(lines)
lines.insert((j + 1) if j is not None else len(lines), flt_line)

# load is not part of the ordered rule sections and may sit at the end.
lines.append(load_line)

open(pfconf, "w").write("\n".join(lines) + "\n")
print("  spliced nat-anchor, anchor and load into %s" % pfconf)
PYEOF

# Validate BEFORE applying. -n parses without loading, so an ordering mistake
# is caught here rather than taking the live ruleset down - which is exactly
# what the first attempt did.
#
# The ALTQ and "-f option" lines are unconditional macOS noise, not errors, so
# they are filtered out of the report while the exit status is judged on its
# own.
if ! pf_err=$(pfctl -n -f "$PFCONF" 2>&1); then
  print -u2 "pf.conf does not parse - restoring the backup and aborting:"
  print -u2 -- "${pf_err}" | grep -vE "ALTQ|Use of -f option|could result in flushing|See /etc/pf.conf" >&2 || true
  cp "$(ls -t $PFCONF.bak.* | head -1)" "$PFCONF"
  exit 1
fi
print "pf.conf parses cleanly"

pfctl -E 2>/dev/null || true
pfctl -f "$PFCONF" 2>&1 | grep -vE "ALTQ|Use of -f option|could result in flushing|See /etc/pf.conf" || true
print "pf reloaded"

print "=== anchor rules now loaded ==="
pfctl -a "$ANCHOR" -s nat 2>/dev/null || true
pfctl -a "$ANCHOR" -s rules 2>/dev/null || true

# The regression that would matter most: these rules must not cost the
# container its own outbound access. Check explicitly and self-revert.
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
