# Phase 0 — Foundations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put the Home Assistant stack under version control with a validated GitOps deploy pipeline, upgrade HA, and create the camera and energy entities that Phases 1–3 depend on.

**Architecture:** `~/ha` on the mac mini becomes a git working clone of a private GitHub repo, edited from a laptop clone and deployed by a polling launchd agent that validates config before reloading and rolls back on failure. Cameras enter HA through native Reolink and ONVIF integrations rather than Scrypted. HA's built-in Energy Dashboard is configured rather than recreated.

**Tech Stack:** Home Assistant 2026.4.x (container, OrbStack), git + GitHub CLI, launchd, zsh, Junos (already configured), Python 3 for verification scripts.

## Global Constraints

- Host is the mac mini `192.168.15.4`, user `nickp`. All commands run over `ssh 192.168.15.4` unless the step says LAPTOP.
- Docker binary is **`~/.orbstack/bin/docker`** — it is NOT on the default PATH. launchd jobs must use the absolute path.
- The container name is `homeassistant`; HA config is bind-mounted at `/config` inside it, `~/ha/homeassistant/config` outside.
- **No secret may ever be committed.** Secrets live only in `secrets.yaml` files, which are gitignored. Every task that commits must pass the secret-scan gate in Task 1.
- Feed-in tariff is **0**. Never select the inverter's `Export Priority` storage mode.
- Do not change any inverter register value in this phase. Phase 0 only reads and configures HA.
- Camera credentials are being rotated by the user on the evening of 2026-07-28. Use `!secret` references so rotation is a single-file edit.
- Repo is `nicko170/home-assistant`, **private**. GitHub auth (`gh`) exists on the LAPTOP only, as user `nicko170`.
- HA restarts take 30–60s. Always wait and verify rather than assuming success.

---

### Task 1: Sanitise ESPHome secrets

Nine ESPHome YAML files contain plaintext wifi SSID, wifi password, OTA passwords and API encryption keys. These must become `!secret` references **before the first content commit**. Git history is currently clean (2 commits, only `.gitignore` and the spec) and must stay that way.

**Files:**
- Create: `/tmp/secretscan.py` (verification gate, reused by later tasks)
- Modify: `homeassistant/config/esphome/power-monitor-1.yaml`, `power-monitor-2.yaml`, `power-monitor-3-f257d8.yaml`, `power-monitor-4.yaml`, `msb-power-monitor.yaml`, `actron-controller.yaml`, `archive/garden-water.yaml`, `archive/esphome-web-338b00.yaml`, `archive/power-monitor-3-f257d8-f257d8.yaml`
- Modify: `esphome/config/power-monitor-2-f27486.yaml`
- Modify: `homeassistant/config/esphome/secrets.yaml`, `esphome/config/secrets.yaml`

**Interfaces:**
- Produces: `/tmp/secretscan.py` exits non-zero if any literal secret remains. Every later commit task runs it as a gate.

- [ ] **Step 1: Write the verification gate**

```bash
ssh 192.168.15.4 'cat > /tmp/secretscan.py' <<'PYEOF'
import os, re, glob, sys
DIRS = ["/Users/nickp/ha/esphome/config",
        "/Users/nickp/ha/homeassistant/config/esphome",
        "/Users/nickp/ha/homeassistant/config"]
SENSITIVE = re.compile(
    r"^\s*(password|passwd|key|api_key|encryption_key|psk|ssid|token)\s*:\s*(.*)$", re.I)
bad = 0
seen = set()
for d in DIRS:
    for path in sorted(glob.glob(os.path.join(d, "**", "*.yaml"), recursive=True)):
        if path in seen or os.path.basename(path) == "secrets.yaml":
            continue
        seen.add(path)
        for n, line in enumerate(open(path, errors="replace").read().splitlines(), 1):
            m = SENSITIVE.match(line)
            if not m:
                continue
            val = m.group(2).strip()
            if not val or val.startswith("#"):
                continue
            if not val.startswith(("!secret", "!env_var")):
                print("LITERAL  %s:%d  %s" % (path.replace("/Users/nickp/ha/", ""), n, m.group(1)))
                bad += 1
print("literals found:", bad)
sys.exit(1 if bad else 0)
PYEOF
```

- [ ] **Step 2: Run the gate to confirm it FAILS**

Run: `ssh 192.168.15.4 'python3 /tmp/secretscan.py; echo "exit=$?"'`
Expected: a list of `LITERAL` lines across the 9 files, `literals found: 20`-ish, `exit=1`.

- [ ] **Step 3: Record the current literal values into the secrets files**

Read each literal value and append it to the matching `secrets.yaml`. Do this by hand, per file, so values are never echoed to the terminal. Keys to create in `homeassistant/config/esphome/secrets.yaml`:

```yaml
wifi_ssid: "<existing literal from power-monitor-1.yaml>"
wifi_password: "<existing literal>"
ota_password: "<existing literal>"
pm1_api_key: "<existing key from power-monitor-1.yaml>"
pm2_api_key: "<existing key from power-monitor-2.yaml>"
pm3_api_key: "<existing key from power-monitor-3-f257d8.yaml>"
pm4_api_key: "<existing key from power-monitor-4.yaml>"
msb_api_key: "<existing key from msb-power-monitor.yaml>"
actron_api_key: "<existing key from actron-controller.yaml>"
```

Mirror `wifi_ssid`, `wifi_password`, `ota_password` and `pm2_api_key` into `esphome/config/secrets.yaml` for `power-monitor-2-f27486.yaml`.

- [ ] **Step 4: Replace literals with `!secret` references**

In each device file, replace the literal values:

```yaml
api:
  encryption:
    key: !secret pm1_api_key

ota:
  - platform: esphome
    password: !secret ota_password

wifi:
  ssid: !secret wifi_ssid
  password: !secret wifi_password
```

Use the per-device key name (`pm1_api_key`, `pm2_api_key`, …). Archived files under `esphome/archive/` get the same treatment — they are still committed, so they still leak.

- [ ] **Step 5: Run the gate to verify it PASSES**

Run: `ssh 192.168.15.4 'python3 /tmp/secretscan.py; echo "exit=$?"'`
Expected: `literals found: 0`, `exit=0`.

- [ ] **Step 6: Verify secrets files are gitignored**

Run: `ssh 192.168.15.4 'cd ~/ha && git check-ignore -v homeassistant/config/esphome/secrets.yaml esphome/config/secrets.yaml homeassistant/config/secrets.yaml'`
Expected: all three print a matching `.gitignore` rule. If any prints nothing it is NOT ignored — stop and fix `.gitignore` before continuing.

- [ ] **Step 7: Commit**

```bash
ssh 192.168.15.4 'cd ~/ha && git add homeassistant/config/esphome esphome/config && git status --short'
# Confirm NO secrets.yaml appears in the staged list, then:
ssh 192.168.15.4 'cd ~/ha && git commit -q -m "refactor(esphome): replace plaintext wifi/OTA/API secrets with !secret refs

Nine device configs held literal wifi credentials, OTA passwords and API
encryption keys. Converted to !secret references so nothing sensitive can
enter git history."'
```

---

### Task 2: First full content commit

Commit the rest of the stack — HA config, ESPHome configs, compose file — with a verified-clean tree. The earlier `git add -A` accident staged 10,924 files including an 875 MB volume; the `.gitignore` must be proven correct before committing.

**Files:**
- Modify: `~/ha/.gitignore` (if gaps found)
- Commit: `docker-compose.yml`, `homeassistant/config/**`, `esphome/config/**`

- [ ] **Step 1: Stage everything and inspect the count**

```bash
ssh 192.168.15.4 'cd ~/ha && git add -A && git diff --cached --name-only | wc -l'
```
Expected: a few hundred at most. **If this exceeds ~1000, STOP** — a volume directory is not ignored. Run `git reset`, fix `.gitignore`, repeat.

- [ ] **Step 2: Prove nothing sensitive or bulky is staged**

```bash
ssh 192.168.15.4 'cd ~/ha && git diff --cached --name-only | grep -iE "secrets\.yaml|\.storage/|\.db|\.log|^scrypted/|^nodered/data/|custom_components/|www/community/" || echo "CLEAN"'
```
Expected: `CLEAN`. Any output is a blocker — reset and fix `.gitignore`.

- [ ] **Step 3: Re-run the secret gate**

Run: `ssh 192.168.15.4 'python3 /tmp/secretscan.py; echo "exit=$?"'`
Expected: `exit=0`.

- [ ] **Step 4: Commit**

```bash
ssh 192.168.15.4 'cd ~/ha && git commit -q -m "feat: track HA stack config in git

Adds docker-compose.yml, Home Assistant config and ESPHome device configs.
Service volumes, databases, logs, .storage and all secrets are excluded via
.gitignore." && git log --oneline'
```

- [ ] **Step 5: Verify repo size is sane**

Run: `ssh 192.168.15.4 'du -sh ~/ha/.git'`
Expected: single-digit MB. If hundreds of MB, a volume was committed — stop and rewrite history before pushing.

---

### Task 3: Create the private GitHub repo and wire up both clones

**Files:**
- Create: LAPTOP `~/code/home-assistant` (clone)
- Create: mac mini `~/.ssh/ha_deploy` + `ha_deploy.pub` (deploy key)
- Modify: mac mini `~/ha/.git/config` (remote)

**Interfaces:**
- Produces: remote `origin` on both clones pointing at `git@github.com:nicko170/home-assistant.git`; mac mini authenticates with a read-only deploy key.

- [ ] **Step 1: Create the empty private repo (LAPTOP)**

```bash
gh repo create nicko170/home-assistant --private --description "Home Assistant stack config (GitOps deployed to mac mini)"
```
Expected: prints the new repo URL. Verify with `gh repo view nicko170/home-assistant --json isPrivate` → `{"isPrivate":true}`.

- [ ] **Step 2: Generate a deploy key on the mac mini**

```bash
ssh 192.168.15.4 'ssh-keygen -t ed25519 -f ~/.ssh/ha_deploy -N "" -C "ha-deploy-macmini" && cat ~/.ssh/ha_deploy.pub'
```

- [ ] **Step 3: Register the key as READ-ONLY on the repo (LAPTOP)**

```bash
gh repo deploy-key add /dev/stdin --repo nicko170/home-assistant --title "macmini-ha-deploy" <<< "<paste the .pub contents from Step 2>"
gh repo deploy-key list --repo nicko170/home-assistant
```
Expected: one key listed. It must NOT be marked read-write — omit `--allow-write`.

- [ ] **Step 4: Configure ssh to use the deploy key**

```bash
ssh 192.168.15.4 'cat >> ~/.ssh/config <<EOF

Host github.com-ha
  HostName github.com
  User git
  IdentityFile ~/.ssh/ha_deploy
  IdentitiesOnly yes
EOF
chmod 600 ~/.ssh/config'
```

- [ ] **Step 5: Bootstrap the first push from the LAPTOP**

The commits exist only on the mac mini, but the mac mini's deploy key is read-only and cannot push. The laptop has full `gh`/SSH auth. So seed GitHub from the laptop by cloning the mac mini over SSH, then repointing at GitHub:

```bash
ssh 192.168.15.4 'cd ~/ha && git branch -M main'
mkdir -p ~/code
git clone ssh://nickp@192.168.15.4/Users/nickp/ha ~/code/home-assistant
cd ~/code/home-assistant
git remote set-url origin git@github.com:nicko170/home-assistant.git
git push -u origin main
```
Expected: push succeeds; `git log --oneline` on the laptop shows the Task 1–2 commits.

- [ ] **Step 6: Point the mac mini at GitHub, pull-only**

```bash
ssh 192.168.15.4 'cd ~/ha && git remote add origin git@github.com-ha:nicko170/home-assistant.git && git fetch origin && git branch --set-upstream-to=origin/main main'
```
Expected: fetch succeeds with no password prompt (the deploy key is used via the `github.com-ha` host alias).

- [ ] **Step 7: Confirm the mac mini genuinely cannot push**

```bash
ssh 192.168.15.4 'cd ~/ha && git push origin main 2>&1 | tail -3'
```
Expected: a permission/read-only error. **This is a success condition** — it proves the deploy key is read-only, so a compromised mac mini cannot rewrite the repo. If the push succeeds, the key was added with write access: remove it and re-add without `--allow-write`.

- [ ] **Step 8: Verify both clones agree**

```bash
ssh 192.168.15.4 'cd ~/ha && git rev-parse HEAD'
cd ~/code/home-assistant && git rev-parse HEAD
```
Expected: identical SHAs.

---

### Task 4: GitOps deploy pipeline with validation and rollback

The core safety mechanism: a bad commit must never be able to leave HA broken.

**Files:**
- Create: `~/ha-deploy/deploy.sh`
- Create: `~/Library/LaunchAgents/net.npratley.ha-deploy.plist`
- Create: `~/.config/ha-deploy/token` (mode 600, gitignored — outside the repo)

**Interfaces:**
- Consumes: repo at `~/ha` with remote `origin` (Task 3).
- Produces: `~/ha-deploy/deploy.sh` — pulls, validates, restarts or rolls back; logs to `~/ha-deploy/deploy.log`.

- [ ] **Step 1: Create a long-lived access token**

In the HA UI: profile → Security → Long-lived access tokens → Create Token, name it `ha-deploy`. Then:

```bash
ssh 192.168.15.4 'mkdir -p ~/.config/ha-deploy && chmod 700 ~/.config/ha-deploy'
ssh 192.168.15.4 'cat > ~/.config/ha-deploy/token && chmod 600 ~/.config/ha-deploy/token' <<< "<paste token>"
```

- [ ] **Step 2: Write the deploy script**

```bash
ssh 192.168.15.4 'mkdir -p ~/ha-deploy && cat > ~/ha-deploy/deploy.sh' <<'EOF'
#!/bin/zsh
set -uo pipefail
REPO=/Users/nickp/ha
DOCKER=/Users/nickp/.orbstack/bin/docker
LOG=/Users/nickp/ha-deploy/deploy.log
TOKEN=$(cat /Users/nickp/.config/ha-deploy/token 2>/dev/null)

log() { echo "$(date '+%F %T') $*" >> "$LOG"; }

notify() {
  [ -z "$TOKEN" ] && return 0
  curl -s -m 10 -X POST \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d "{\"message\": \"$1\", \"title\": \"HA Deploy\"}" \
    http://127.0.0.1:8123/api/services/notify/mobile_app_nicks_iphone >/dev/null
}

cd "$REPO" || exit 1
git fetch --quiet origin main || { log "fetch failed"; exit 1; }
LOCAL=$(git rev-parse HEAD)
REMOTE=$(git rev-parse origin/main)
[ "$LOCAL" = "$REMOTE" ] && exit 0

log "deploying $LOCAL -> $REMOTE"
git merge --ff-only origin/main >> "$LOG" 2>&1 || {
  log "ff-only merge failed - local diverged"; notify "Deploy FAILED: local diverged"; exit 1; }

if ! $DOCKER exec homeassistant python -m homeassistant --script check_config -c /config >> "$LOG" 2>&1; then
  log "check_config FAILED - rolling back to $LOCAL"
  git reset --hard "$LOCAL" >> "$LOG" 2>&1
  notify "Deploy FAILED, rolled back. Config invalid."
  exit 1
fi

$DOCKER restart homeassistant >> "$LOG" 2>&1
SHORT=$(git rev-parse --short HEAD)
log "deployed $SHORT"
notify "Deployed $SHORT"
EOF
ssh 192.168.15.4 'chmod +x ~/ha-deploy/deploy.sh'
```

- [ ] **Step 3: Run it manually with no changes pending**

Run: `ssh 192.168.15.4 '~/ha-deploy/deploy.sh; echo "exit=$?"'`
Expected: `exit=0`, no restart, nothing appended to the log (already up to date).

- [ ] **Step 4: Test the happy path**

From LAPTOP, make a harmless change and push:

```bash
cd ~/code/home-assistant && echo "# gitops deploy test" >> docs/superpowers/specs/2026-07-28-ha-rebuild-design.md
git commit -aqm "test: gitops deploy happy path" && git push
```

Then: `ssh 192.168.15.4 '~/ha-deploy/deploy.sh; tail -5 ~/ha-deploy/deploy.log'`
Expected: log shows `deployed <sha>`, container restarts, phone notification arrives.

- [ ] **Step 5: Test the rollback path — this is the critical test**

From LAPTOP, push a deliberately broken config:

```bash
cd ~/code/home-assistant && printf '\nthis_is_not_valid_ha_config:\n  - [[[\n' >> homeassistant/config/configuration.yaml
git commit -aqm "test: deliberately broken config for rollback test" && git push
```

Then: `ssh 192.168.15.4 '~/ha-deploy/deploy.sh; echo "exit=$?"; tail -8 ~/ha-deploy/deploy.log'`
Expected: `check_config FAILED - rolling back`, `exit=1`, phone notification says rolled back.

Verify HA is still healthy and on the previous commit:
```bash
ssh 192.168.15.4 'cd ~/ha && git log --oneline -1 && ~/.orbstack/bin/docker ps --format "{{.Names}} {{.Status}}" | grep homeassistant'
```
Expected: HEAD is the *previous* good commit; container is `Up`.

- [ ] **Step 6: Revert the broken commit (LAPTOP)**

```bash
cd ~/code/home-assistant && git revert --no-edit HEAD && git push
ssh 192.168.15.4 '~/ha-deploy/deploy.sh; tail -3 ~/ha-deploy/deploy.log'
```
Expected: deploys cleanly.

- [ ] **Step 7: Install the launchd agent**

```bash
ssh 192.168.15.4 'cat > ~/Library/LaunchAgents/net.npratley.ha-deploy.plist' <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>net.npratley.ha-deploy</string>
    <key>ProgramArguments</key>
    <array><string>/Users/nickp/ha-deploy/deploy.sh</string></array>
    <key>StartInterval</key><integer>120</integer>
    <key>RunAtLoad</key><true/>
    <key>StandardOutPath</key><string>/Users/nickp/ha-deploy/launchd.out.log</string>
    <key>StandardErrorPath</key><string>/Users/nickp/ha-deploy/launchd.err.log</string>
</dict>
</plist>
EOF
ssh 192.168.15.4 'launchctl bootstrap gui/501 ~/Library/LaunchAgents/net.npratley.ha-deploy.plist && launchctl list | grep ha-deploy'
```
Expected: the job is listed with exit status 0.

- [ ] **Step 8: End-to-end test via launchd**

Push a trivial change from LAPTOP, wait ~3 minutes, then check the log without running the script by hand:
```bash
ssh 192.168.15.4 'tail -5 ~/ha-deploy/deploy.log'
```
Expected: an automatic `deployed <sha>` entry.

- [ ] **Step 9: Commit the deploy tooling**

The deploy script lives outside the repo (it must survive a bad commit), so copy it in for reference only:

```bash
ssh 192.168.15.4 'mkdir -p ~/ha/deploy && cp ~/ha-deploy/deploy.sh ~/ha/deploy/deploy.sh.reference && cp ~/Library/LaunchAgents/net.npratley.ha-deploy.plist ~/ha/deploy/'
ssh 192.168.15.4 'cd ~/ha && git add deploy && git commit -qm "feat(deploy): add GitOps deploy script and launchd agent

Polls origin/main every 120s, validates with check_config before restarting
HA, and hard-resets to the previous commit if validation fails. The live
script runs from ~/ha-deploy so a bad commit cannot break the deployer
itself; this copy is reference only." && git push'
```

---

### Task 5: Backup and upgrade HA 2026.1.0 → 2026.4.x

**Files:**
- Create: `~/ha-backups/ha-config-2026-07-28.tar.gz`
- Modify: `docker-compose.yml` only if the image tag is pinned (it is `:stable`, so likely no change)

- [ ] **Step 1: Stop HA and take a full config backup**

```bash
ssh 192.168.15.4 'mkdir -p ~/ha-backups && ~/.orbstack/bin/docker stop homeassistant && tar czf ~/ha-backups/ha-config-2026-07-28.tar.gz -C ~/ha/homeassistant config && ls -lh ~/ha-backups/'
```
Expected: a tarball of a few hundred MB. **Do not proceed without this.**

- [ ] **Step 2: Record the current version and integration list**

```bash
ssh 192.168.15.4 'cat ~/ha/homeassistant/config/.HA_VERSION'
```
Expected: `2026.1.0`. Note it — this is the rollback target.

- [ ] **Step 3: Pull the new image and start**

```bash
ssh 192.168.15.4 'cd ~/ha && ~/.orbstack/bin/docker compose pull homeassistant && ~/.orbstack/bin/docker compose up -d homeassistant'
```

- [ ] **Step 4: Wait for startup and confirm the version**

```bash
ssh 192.168.15.4 'sleep 90; cat ~/ha/homeassistant/config/.HA_VERSION; ~/.orbstack/bin/docker ps --format "{{.Names}} {{.Status}}" | grep homeassistant'
```
Expected: version is `2026.4.x`, container `Up`.

- [ ] **Step 5: Verify the critical custom components loaded**

```bash
ssh 192.168.15.4 'grep -iE "Error setting up entry|Error during setup of component" ~/ha/homeassistant/config/home-assistant.log | tail -20'
```
Expected: `scrypted` may still error (removed in Task 6). **`solarman` must NOT appear.**

- [ ] **Step 6: Confirm the inverter is still reporting**

```bash
ssh 192.168.15.4 'lsof -nP -iTCP -sTCP:ESTABLISHED | grep 8899'
```
Expected: an established connection to `192.168.15.226:8899`.

**ROLLBACK if solarman failed:**
```bash
ssh 192.168.15.4 '~/.orbstack/bin/docker stop homeassistant && rm -rf ~/ha/homeassistant/config && tar xzf ~/ha-backups/ha-config-2026-07-28.tar.gz -C ~/ha/homeassistant && cd ~/ha && ~/.orbstack/bin/docker compose up -d homeassistant'
```
Then pin the image to `2026.1.0` in `docker-compose.yml` and stop — the upgrade needs separate investigation.

- [ ] **Step 7: Commit any config migrations HA made**

```bash
ssh 192.168.15.4 'cd ~/ha && git status --short && python3 /tmp/secretscan.py'
ssh 192.168.15.4 'cd ~/ha && git add -A && git commit -qm "chore: HA 2026.1.0 -> 2026.4.x config migrations" && git push'
```

---

### Task 6: Remove the broken Scrypted integration and add Reolink cameras

Scrypted's HA component crashes on registration (`TypeError: unhashable type: 'dict'`) and only provides a sidebar panel, not camera entities. Native Reolink gives doorbell press events, person/vehicle detection and chime control.

**Files:**
- Modify: `homeassistant/config/.storage/core.config_entries` (via UI, not by hand while running)
- Modify: `homeassistant/config/secrets.yaml`

- [ ] **Step 1: Add camera credentials to secrets**

```bash
ssh 192.168.15.4 'cat >> ~/ha/homeassistant/config/secrets.yaml' <<'EOF'
camera_username: admin
camera_password: "<current password; user rotates tonight>"
EOF
```
Confirm it is ignored: `ssh 192.168.15.4 'cd ~/ha && git check-ignore -v homeassistant/config/secrets.yaml'` → must print a rule.

- [ ] **Step 2: Remove the Scrypted integration**

In the HA UI: Settings → Devices & Services → Scrypted → ⋮ → Delete. Then verify:
```bash
ssh 192.168.15.4 'grep -c "for scrypted" ~/ha/homeassistant/config/home-assistant.log'
```
Restart HA and confirm the error no longer appears. Scrypted itself keeps running natively for HomeKit — do not stop it.

- [ ] **Step 3: Add the Reolink doorbell (.240)**

UI: Settings → Devices & Services → Add Integration → **Reolink** → host `192.168.15.240`, username/password as above.

- [ ] **Step 4: Add the Reolink CX820 (.245)**

Same flow with host `192.168.15.245`.

- [ ] **Step 5: Verify camera and doorbell entities now exist**

```bash
ssh 192.168.15.4 'python3 - <<PYEOF
import json
C="/Users/nickp/ha/homeassistant/config/.storage/"
e=json.load(open(C+"core.entity_registry"))["data"]["entities"]
cams=[x["entity_id"] for x in e if x["entity_id"].startswith("camera.")]
bells=[x["entity_id"] for x in e if "doorbell" in x["entity_id"] or "visitor" in x["entity_id"]]
print("cameras:", len(cams)); [print("  ", c) for c in cams]
print("doorbell-ish:"); [print("  ", b) for b in bells]
PYEOF'
```
Expected: at least 2 `camera.*` entities, and a doorbell/visitor binary_sensor for `.240`. **This is the gate for Phase 2's Cameras dashboard.**

- [ ] **Step 6: Commit**

```bash
ssh 192.168.15.4 'cd ~/ha && git add -A && git commit -qm "feat(cameras): replace broken Scrypted integration with native Reolink

Scrypted HA component crashes on 2026.x registering its panel and provides
no camera entities. Native Reolink adds doorbell press, person/vehicle
detection and chime for .240 and .245. Scrypted continues bridging to
HomeKit natively." && git push'
```

---

### Task 7: Add the Hikvision cameras via ONVIF

Firmware is V5.4.800 (old). ONVIF may need enabling on the camera itself, and may not work at all — the fallback is stream-only.

**Files:**
- Modify: HA config entries via UI

- [ ] **Step 1: Verify ONVIF is reachable on each camera**

```bash
ssh 192.168.15.4 'python3 - <<PYEOF
import socket
for ip in ("192.168.15.241","192.168.15.242","192.168.15.244"):
    for port in (80, 8000, 2020):
        s=socket.socket(); s.settimeout(3)
        try:
            s.connect((ip,port)); print("  %s:%d OPEN" % (ip,port))
        except Exception as ex: print("  %s:%d %s" % (ip,port,type(ex).__name__))
        finally: s.close()
PYEOF'
```
Expected: port 80 open on all three. ONVIF commonly lives on 80 or 8000 for Hikvision.

- [ ] **Step 2: Add each camera via the ONVIF integration**

UI: Add Integration → **ONVIF** → host `192.168.15.241`, port 80, username/password. Repeat for `.242` and `.244`.

If ONVIF fails on a camera, enable it on the camera's own web UI (Configuration → Network → Advanced → Integration Protocol → Enable ONVIF, and add an ONVIF user) and retry.

- [ ] **Step 3: If ONVIF still fails, fall back to stream-only**

Add via **Generic Camera** with the Hikvision RTSP URL:
`rtsp://<user>:<pass>@192.168.15.241:554/Streaming/Channels/101`
Note in the commit message that motion events are unavailable for that camera.

- [ ] **Step 4: Verify entity count**

```bash
ssh 192.168.15.4 'python3 - <<PYEOF
import json
C="/Users/nickp/ha/homeassistant/config/.storage/"
e=json.load(open(C+"core.entity_registry"))["data"]["entities"]
print("cameras:", len([x for x in e if x["entity_id"].startswith("camera.")]))
PYEOF'
```
Expected: 5 cameras total (.240, .241, .242, .244, .245). `.243` is dead hardware and stays absent.

- [ ] **Step 5: Commit**

```bash
ssh 192.168.15.4 'cd ~/ha && git add -A && git commit -qm "feat(cameras): add Hikvision cameras via ONVIF" && git push'
```

---

### Task 8: Configure the built-in Energy Dashboard

Use `total_increasing` sensors. The `today_*` variants reset daily and behave incorrectly as energy dashboard sources.

**Files:**
- Modify: `homeassistant/config/.storage/energy` (via UI)

- [ ] **Step 1: Confirm state_class on the sensors before wiring them**

```bash
ssh 192.168.15.4 'python3 - <<PYEOF
import sqlite3
DB="/Users/nickp/ha/homeassistant/config/home-assistant_v2.db"
con=sqlite3.connect("file:%s?mode=ro" % DB, uri=True)
q="""SELECT sm.entity_id, sa.shared_attrs FROM states s
JOIN states_meta sm ON sm.metadata_id=s.metadata_id
JOIN state_attributes sa ON sa.attributes_id=s.attributes_id
WHERE sm.entity_id IN (
 'sensor.inverter_total_energy_import','sensor.inverter_total_energy_export',
 'sensor.inverter_total_production','sensor.inverter_total_battery_charge',
 'sensor.inverter_total_battery_discharge')
GROUP BY sm.entity_id"""
for eid, attrs in con.execute(q):
    print("  %-46s %s" % (eid, "total_increasing" if "total_increasing" in (attrs or "") else "CHECK: "+str(attrs)[:60]))
PYEOF'
```
Expected: all five report `total_increasing`. If any does not, use the corresponding `today_*` sensor with a Utility Meter helper instead, and note it.

- [ ] **Step 2: Configure the Energy Dashboard**

UI: Settings → Dashboards → Energy:
- Grid consumption → `sensor.inverter_total_energy_import`
- Return to grid → `sensor.inverter_total_energy_export`
- Solar production → `sensor.inverter_total_production`
- Battery in → `sensor.inverter_total_battery_charge`
- Battery out → `sensor.inverter_total_battery_discharge`

Tariff: enter Origin **placeholder** rates. Set **feed-in tariff to 0** — this is confirmed, not a placeholder.

- [ ] **Step 3: Add individual devices**

Add each MSB sub-circuit `sensor.msb_meter_energy_sub01` … `sub16` (skip any unavailable while the meter is offline) and each power monitor's `..._total_energy`.

- [ ] **Step 4: Verify the dashboard renders with data**

Open the Energy dashboard. Expected: solar, grid and battery show today's figures. If everything reads zero, the sensors are wrong — recheck Step 1.

- [ ] **Step 5: Commit**

```bash
ssh 192.168.15.4 'cd ~/ha && git add -A && git commit -qm "feat(energy): configure built-in Energy Dashboard

Uses total_increasing inverter sensors for grid/solar/battery, MSB
sub-circuits and power monitors as individual devices. Feed-in tariff set
to 0; import rates are placeholders pending real Origin figures." && git push'
```

---

### Task 9: Cleanup

**Files:**
- Modify: `homeassistant/config/automations.yaml`
- Modify: HA config entries via UI

- [ ] **Step 1: Delete the Garden Water Timer integration**

UI: Settings → Devices & Services → ESPHome → "Garden Water Timer" → Delete. The device is offline and being retired.

- [ ] **Step 2: Remove the broken automation**

Delete the `Water Front Gardens` automation (id `1756041019076`) from `automations.yaml` — it references a deleted device and is already disabled.

```bash
ssh 192.168.15.4 'grep -n "Water Front Gardens" ~/ha/homeassistant/config/automations.yaml'
```
Remove that entire list item, then verify the file still parses:
```bash
ssh 192.168.15.4 'python3 -c "import yaml,sys; yaml.safe_load(open(\"/Users/nickp/ha/homeassistant/config/automations.yaml\")); print(\"YAML OK\")"'
```

- [ ] **Step 3: Disable the Actron Controller entry**

UI: ESPHome → "Actron Controller" → ⋮ → Disable. The device is offline pending the user's PCB build; disabling stops it erroring. **Do not delete** — it returns later.

- [ ] **Step 4: Archive the dead Solar Monitor ESPHome config**

```bash
ssh 192.168.15.4 'cd ~/ha/homeassistant/config/esphome && ls archive/solar-monitor-da990c.yaml'
```
It is already in `archive/`. Leave it; it is sanitised and harmless.

- [ ] **Step 5: Validate config and verify the error log is quiet**

```bash
ssh 192.168.15.4 '~/.orbstack/bin/docker exec homeassistant python -m homeassistant --script check_config -c /config 2>&1 | tail -5'
ssh 192.168.15.4 '~/.orbstack/bin/docker restart homeassistant && sleep 75 && grep -cE "ERROR" ~/ha/homeassistant/config/home-assistant.log'
```
Expected: `check_config` reports no errors. Remaining ERROR count should be lower than before; Plex/Roborock/Matter remain until the user reauthenticates.

- [ ] **Step 6: Commit**

```bash
ssh 192.168.15.4 'cd ~/ha && git add -A && git commit -qm "chore: remove retired Garden Water Timer and its broken automation

Disables the offline Actron Controller entry pending hardware. Solar Monitor
was already deleted at user request." && git push'
```

---

## Phase 0 Exit Criteria

All must hold before Phase 1 begins:

- [ ] `python3 /tmp/secretscan.py` exits 0; no secret has ever been committed
- [ ] `nicko170/home-assistant` is private, contains the stack, `.git` is single-digit MB
- [ ] A push from `~/code/home-assistant` deploys automatically within ~3 minutes
- [ ] A broken commit is rejected by `check_config`, rolled back, and notified — **verified by actual test**, not assumption
- [ ] HA is on 2026.4.x and the inverter is connected on `192.168.15.226:8899`
- [ ] 5 `camera.*` entities exist, including a doorbell press sensor for `.240`
- [ ] The Energy Dashboard renders real solar, grid and battery data with FIT = 0

## Deferred to later phases

- Inverter profile switch to `sofar_g3hyd.yaml` — blocked on the user confirming the physical model
- `Time of Use` / export-limit automation — Phase 3, and only after self-consumption measurement exists
- Node-RED and the stale `scrypted`/`cloudflared`/`watchtower` compose services — user decision pending
