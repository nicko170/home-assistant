"""Secret gate for the HA repo.

Exits non-zero if any tracked-or-trackable file holds a literal secret.
Covers YAML (!secret refs) and docker-compose/env style KEY=VALUE.
Never prints secret values.
"""
import os, re, glob, sys, subprocess

ROOT = "/Users/nickp/ha"

YAML_DIRS = [os.path.join(ROOT, "esphome/config"),
             os.path.join(ROOT, "homeassistant/config/esphome"),
             os.path.join(ROOT, "homeassistant/config")]

YAML_SENS = re.compile(
    r"^\s*(password|passwd|key|api_key|encryption_key|psk|ssid|token)\s*:\s*(.*)$", re.I)

ENV_SENS = re.compile(
    r"^\s*-?\s*([A-Z0-9_]*(TOKEN|PASSWORD|SECRET|AUTHORIZATION|APIKEY|API_KEY)[A-Z0-9_]*)\s*=\s*(.*)$")

bad = []


def ignored(path):
    """Skip anything git already ignores - it can never be committed."""
    r = subprocess.run(["git", "-C", ROOT, "check-ignore", "-q", path])
    return r.returncode == 0


# --- YAML: every sensitive key must be a !secret / !env_var reference ---
seen = set()
for d in YAML_DIRS:
    for path in sorted(glob.glob(os.path.join(d, "**", "*.yaml"), recursive=True)):
        if path in seen or os.path.basename(path) == "secrets.yaml" or ignored(path):
            continue
        seen.add(path)
        for n, line in enumerate(open(path, errors="replace").read().splitlines(), 1):
            m = YAML_SENS.match(line)
            if not m:
                continue
            val = m.group(2).strip()
            if not val or val.startswith("#"):
                continue
            if not val.startswith(("!secret", "!env_var")):
                bad.append("%s:%d  %s" % (path.replace(ROOT + "/", ""), n, m.group(1)))

# --- compose/env: sensitive vars must be ${REFERENCES}, not literals ---
for path in sorted(glob.glob(os.path.join(ROOT, "*.yml")) + glob.glob(os.path.join(ROOT, "*.yaml"))):
    if ignored(path):
        continue
    for n, line in enumerate(open(path, errors="replace").read().splitlines(), 1):
        if line.strip().startswith("#"):
            continue
        m = ENV_SENS.match(line)
        if not m:
            continue
        val = m.group(3).strip()
        if val and not val.startswith("${"):
            bad.append("%s:%d  %s (literal, should be ${...})" % (
                path.replace(ROOT + "/", ""), n, m.group(1)))

for b in bad:
    print("LITERAL  %s" % b)
print("literals found:", len(bad))
sys.exit(1 if bad else 0)
