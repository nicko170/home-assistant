# Family Dashboard — Phases 0–2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up every external dependency, turn the Galaxy Tab into a self-managing appliance, and ship an empty-but-navigable five-tab family dashboard — so that Phases 3–7 only ever add content to a working shell.

**Architecture:** Integrations own data, packages own behaviour, dashboard files own presentation and contain no logic. The dashboard is a shell (`dashboards/family.yaml`) that `!include`s one file per view, each ending with a shared `navbar-card`. A new local validation harness runs the real Home Assistant image against the config before pushing, catching a class of error the mac mini's deploy gate lets through.

**Tech Stack:** Home Assistant 2026.7.4 (Docker, OrbStack, compose project `ha` at `/Users/nickp/ha`), HACS, `navbar-card`, `kiosk-mode`, MS365-Calendar, ChoreOps, Mealie, Fully Kiosk Browser.

**Spec:** `docs/superpowers/specs/2026-08-23-family-dashboard-design.md`

## Global Constraints

- **HA version is 2026.7.4.** Pin the validation image to the version in `homeassistant/config/.HA_VERSION`, never `stable`.
- **Lovelace is `mode: yaml`.** Every HACS frontend card MUST be declared in `lovelace.resources` in `configuration.yaml` or it silently fails on *every* dashboard. HACS cannot self-register in YAML mode.
- **Resource filenames are not guessable.** The repo already documents one case (`ha-sankey-chart.js`, not `sankey-chart.js`). Always `ls` the installed directory on the mac mini before writing a resource URL.
- **Dashboard `url_path` must contain a hyphen** (HA requirement). The new dashboard key is `home-family`, matching the existing `home-energy` / `home-cameras` / `home-admin`.
- **Features are packaged.** Helpers, templates and automations for one feature live in a single `packages/*.yaml`. Never scatter them.
- **Secrets go through `!secret`.** `tools/secret-gate.py` fails the commit otherwise.
- **Deploy is GitOps.** Push to `main`; the mac mini pulls within ~2 min, runs `check_config` in-container, and hard-resets on a *parse* error only.
- **`deploy.sh` restarts Home Assistant only.** It does not run `docker compose up`. Any new compose service must be started manually on the mac mini once.
- **Per-person colours (verbatim from spec):** Nick `#2E86DE`, Elle `#A55EEA`, Aria `#FF6B9D`, Eden `#26C6A0`, shared `#F7B731`.
- **O365 tenant** is `pratley.au`, self-owned, Nick is admin. Accounts `nick@pratley.au`, `elle@pratley.au`.
- **Notify targets:** Nick is `notify.mobile_app_nicks_iphone`; Elle's companion device is named just `iPhone`, so hers is `notify.mobile_app_iphone`.
- **Mac mini access:** `ssh 192.168.15.4`. Docker is not on the non-interactive PATH — prefix commands with `export PATH="$HOME/.orbstack/bin:$PATH"`.

## Known Pre-Existing Issue (not in scope)

`check_config` reports the automation `Turn TV on with Apple TV` fails with `Unknown device '63da27bb84d9fa488f224cff0ec53b7a'`. This is pre-existing, unrelated to this work, and appears in every validation run. Do not treat it as a regression; do not fix it as part of these tasks.

## File Structure

| File | Responsibility |
|---|---|
| `tools/check-config.sh` | Create. Runs real `check_config` locally against a scratch copy of the config with synthesised secrets. The test harness every later task uses. |
| `docker-compose.yml` | Modify. Adds the `mealie` service. |
| `.gitignore` | Modify. Adds `mealie/` alongside `scrypted/` and `nodered/`. |
| `homeassistant/config/configuration.yaml` | Modify. Adds two `lovelace.resources` entries and the `home-family` dashboard. |
| `homeassistant/config/themes/family.yaml` | Create. Colours only. The `themes/` directory does not yet exist. |
| `homeassistant/config/dashboards/family.yaml` | Create. Shell: five `!include`d views, nothing else. |
| `homeassistant/config/dashboards/family/_navbar.yaml` | Create. The single definition of the bottom tab bar. |
| `homeassistant/config/dashboards/family/{home,calendar,chores,kitchen,more}.yaml` | Create. One file per view; placeholders this phase. |
| `homeassistant/config/packages/family_kiosk.yaml` | Create. Tablet behaviour: brightness schedule, idle-return, nightly restart, offline watchdog. |

`!include` resolves relative to the file containing it, so `dashboards/family.yaml` uses `family/home.yaml` and `dashboards/family/home.yaml` uses `_navbar.yaml`.

---

### Task 1: Local config validation harness

The harness the rest of the plan depends on. It must be built and proven first, because every later task's verification step is "run it".

**Files:**
- Create: `tools/check-config.sh`

**Interfaces:**
- Produces: `tools/check-config.sh`, exit 0 on a valid config and non-zero on either a YAML parse error or a schema error. Every later task runs it before committing.

- [ ] **Step 1: Write the harness**

Create `tools/check-config.sh`:

```bash
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

echo "OK: config is valid"
```

Then make it executable:

```bash
chmod +x tools/check-config.sh
```

- [ ] **Step 2: Prove it passes on the current config**

Run: `./tools/check-config.sh`
Expected: ends with `OK: config is valid`, exit 0. The `Turn TV on with Apple TV` ERROR line appears and is expected — see Known Pre-Existing Issue.

- [ ] **Step 3: Prove it fails on a YAML parse error**

```bash
cp homeassistant/config/packages/extras.yaml /tmp/extras.bak
printf '\ninput_boolean:\n  broken: [[[\n' >> homeassistant/config/packages/extras.yaml
./tools/check-config.sh; echo "exit=$?"
cp /tmp/extras.bak homeassistant/config/packages/extras.yaml
```

Expected: `FAIL: check_config exited 1 (YAML parse error)`, `exit=1`.

- [ ] **Step 4: Prove it fails on a schema error — the case deploy.sh misses**

```bash
cp homeassistant/config/packages/extras.yaml /tmp/extras.bak
printf '\ninput_boolean:\n  bad_helper:\n    not_a_real_option: 42\n' >> homeassistant/config/packages/extras.yaml
./tools/check-config.sh; echo "exit=$?"
cp /tmp/extras.bak homeassistant/config/packages/extras.yaml
```

Expected: `FAIL: schema error - deploy.sh would NOT catch this`, `exit=1`.

- [ ] **Step 5: Confirm the config is back to clean**

Run: `git diff --stat homeassistant/config/packages/extras.yaml`
Expected: no output (file restored byte-for-byte).

Run: `./tools/check-config.sh`
Expected: `OK: config is valid`.

- [ ] **Step 5b: Prove the dashboard stage catches a broken `!include`**

```bash
cp homeassistant/config/dashboards/energy.yaml /tmp/energy.bak
sed -i '' '1s|^|!include nonexistent.yaml\n|' homeassistant/config/dashboards/energy.yaml
./tools/check-config.sh; echo "exit=$?"
cp /tmp/energy.bak homeassistant/config/dashboards/energy.yaml
```

Expected: `FAIL: a dashboard file does not parse - it would render blank on the wall`, `exit=1`.

- [ ] **Step 6: Commit**

```bash
git add tools/check-config.sh
git commit -m "feat(tools): local check_config harness, stricter than the deploy gate

check_config exits 0 on config SCHEMA errors, so deploy.sh ships them and the
component silently disables itself. Fail on 'Invalid config for' to catch that
class locally, before pushing."
```

---

### Task 2: Mealie service and its Home Assistant integration

**Files:**
- Modify: `docker-compose.yml`
- Modify: `.gitignore`

**Interfaces:**
- Produces: Mealie reachable at `http://192.168.15.4:9925`, and HA entities `calendar.mealie_dinner` plus today's-meal sensors. Phase 5 consumes these; nothing in Phases 0–2 does.

- [ ] **Step 1: Add the Mealie service**

Append to the `services:` block in `docker-compose.yml`, after `esphome`:

```yaml
  # ============================================================================
  # MEALIE (recipe manager + weekly meal plan)
  # ============================================================================
  # Access: http://<host-ip>:9925
  #
  # Unlike the rest of this stack Mealie uses bridge networking with an explicit
  # port map, not network_mode: host. It needs no mDNS/SSDP discovery, and
  # Mealie's listen port is fixed at 9000 with no env var to change it - so a
  # port map is how we avoid colliding with anything else that wants 9000.
  mealie:
    container_name: mealie
    image: ghcr.io/mealie-recipes/mealie:v3.23.1
    ports:
      - "9925:9000"
    volumes:
      - ./mealie:/app/data
    environment:
      - TZ=Australia/Sydney
      - ALLOW_SIGNUP=false
      - BASE_URL=http://192.168.15.4:9925
    deploy:
      resources:
        limits:
          memory: 1000M
    restart: unless-stopped
```

- [ ] **Step 2: Ignore the Mealie volume**

In `.gitignore`, under `# ---- service volumes (bulk runtime data, not config) ----`, add `mealie/` so the section reads:

```
# ---- service volumes (bulk runtime data, not config) ----
scrypted/
nodered/
mealie/
```

- [ ] **Step 3: Verify the compose file still parses**

Run: `docker compose config --quiet && echo "compose OK"`
Expected: `compose OK`. A warning about `CLOUDFLARE_TUNNEL_TOKEN` being unset is expected and harmless — the `.env` file is not in the repo.

- [ ] **Step 4: Commit and push**

```bash
git add docker-compose.yml .gitignore
git commit -m "feat(mealie): add Mealie service for the family dinner plan

Bridge networking with an explicit 9925:9000 map rather than host networking:
Mealie needs no discovery and its listen port is not configurable."
git push
```

- [ ] **Step 5: Start it on the mac mini — deploy.sh will NOT do this**

`deploy.sh` only restarts the `homeassistant` container. A new compose service must be brought up by hand, once:

```bash
ssh 192.168.15.4 'export PATH="$HOME/.orbstack/bin:$PATH"; cd ~/ha && \
  git pull --ff-only && docker compose up -d mealie && docker compose ps mealie'
```

Expected: `mealie` shows `running`.

- [ ] **Step 6: Verify Mealie answers**

Run: `curl -sS -o /dev/null -w '%{http_code}\n' http://192.168.15.4:9925/`
Expected: `200`.

- [ ] **Step 7: Create the Mealie account and API token**

In a browser at `http://192.168.15.4:9925`: log in with the first-run credentials shown in Mealie's docs, change the password immediately, then create an API token under the user profile. Copy the token.

- [ ] **Step 8: Add the Mealie integration to Home Assistant**

In HA → Settings → Devices & Services → Add Integration → Mealie. Host `http://192.168.15.4:9925`, paste the API token.

- [ ] **Step 9: Verify the entities exist**

```bash
ssh 192.168.15.4 'python3 -c "
import json
d=json.load(open(\"/Users/nickp/ha/homeassistant/config/.storage/core.entity_registry\"))[\"data\"][\"entities\"]
print([e[\"entity_id\"] for e in d if \"mealie\" in e[\"entity_id\"]])
"'
```

Expected: a non-empty list including a `calendar.*` entity and today's-meal `sensor.*` entities. Record the exact IDs — Phase 5 needs them.

---

### Task 3: Frontend cards and their Lovelace resources

**Files:**
- Modify: `homeassistant/config/configuration.yaml` (the `lovelace.resources` list)

**Interfaces:**
- Produces: `custom:navbar-card` and kiosk-mode available on every dashboard. Task 5 consumes `custom:navbar-card`.

- [ ] **Step 1: Install both cards via HACS**

In HA → HACS → search and install:
- `navbar-card` (joseluis9595/lovelace-navbar-card)
- `kiosk-mode` (NemesisRE/kiosk-mode)

Do **not** trust HACS to register them. In `mode: yaml` it cannot.

- [ ] **Step 2: Read the real filenames off disk**

Guessing here is how `ha-sankey-chart` broke before. Run:

```bash
ssh 192.168.15.4 'ls ~/ha/homeassistant/config/www/community/lovelace-navbar-card/ \
                     ~/ha/homeassistant/config/www/community/kiosk-mode/'
```

Expected: a `.js` file in each. Use the **exact** names printed in the next step; if they differ from `navbar-card.js` / `kiosk-mode.js`, use what is printed.

- [ ] **Step 3: Declare the resources**

In `homeassistant/config/configuration.yaml`, append to `lovelace.resources`, immediately before the `ha-sankey-chart` entry and its NOTE comment:

```yaml
    - url: /hacsfiles/lovelace-navbar-card/navbar-card.js
      type: module
    - url: /hacsfiles/kiosk-mode/kiosk-mode.js
      type: module
```

- [ ] **Step 4: Validate**

Run: `./tools/check-config.sh`
Expected: `OK: config is valid`.

- [ ] **Step 5: Commit and push**

```bash
git add homeassistant/config/configuration.yaml
git commit -m "feat(family): register navbar-card and kiosk-mode resources

Lovelace is in yaml mode, so HACS cannot register these itself - undeclared
cards fail silently on every dashboard, not just the new one."
git push
```

- [ ] **Step 6: Verify the cards actually load**

Wait ~2 min for the deploy, then hard-refresh a browser on any HA dashboard and open the devtools console.
Expected: no `Custom element doesn't exist` errors, and no 404s for either `/hacsfiles/` URL.

---

### Task 4: The family theme

**Files:**
- Create: `homeassistant/config/themes/family.yaml`

**Interfaces:**
- Produces: a theme named `family`, exposing `--family-nick`, `--family-elle`, `--family-aria`, `--family-eden`, `--family-shared` as CSS custom properties. Phases 3–7 consume these via card-mod.

`configuration.yaml` already contains `frontend: themes: !include_dir_merge_named themes`, but the `themes/` directory does not exist. Its absence is not an error (verified), and creating it activates the existing include.

- [ ] **Step 1: Create the theme**

Create `homeassistant/config/themes/family.yaml`:

```yaml
# Family kitchen dashboard theme.
#
# Light, not dark. A kitchen in daylight makes a dark dashboard unreadable, and
# Skylight's own identity is bright. Night legibility is handled by dimming the
# tablet backlight in packages/family_kiosk.yaml, not by swapping themes - one
# theme means one set of behaviours to reason about.
#
# Colours only. HA has no reliable global font-size variable, so the "readable
# from 3 m" sizing is done per-card with card-mod in Phases 3-7 rather than
# faked here with paper-font overrides that modern HA cards ignore.
family:
  # --- per-person accents ---------------------------------------------------
  # HA turns every theme key into a CSS custom property with a "--" prefix, so
  # these are usable from card-mod as var(--family-aria) and friends.
  family-nick: "#2E86DE"
  family-elle: "#A55EEA"
  family-aria: "#FF6B9D"
  family-eden: "#26C6A0"
  family-shared: "#F7B731"

  # --- base palette ---------------------------------------------------------
  primary-color: "#2E86DE"
  accent-color: "#F7B731"
  primary-background-color: "#F4F6FA"
  secondary-background-color: "#FFFFFF"
  card-background-color: "#FFFFFF"
  primary-text-color: "#1B2430"
  secondary-text-color: "#5A6779"
  divider-color: "#E3E8EF"
  state-icon-color: "#2E86DE"

  # --- card shape -----------------------------------------------------------
  # Rounder and softer than HA's default: this sits on a wall in a kitchen, not
  # in a control panel.
  ha-card-border-radius: "22px"
  ha-card-border-width: "0"
  ha-card-box-shadow: "0 2px 10px rgba(27, 36, 48, 0.08)"
```

- [ ] **Step 2: Validate**

Run: `./tools/check-config.sh`
Expected: `OK: config is valid`.

- [ ] **Step 3: Commit and push**

```bash
git add homeassistant/config/themes/family.yaml
git commit -m "feat(family): add the family kitchen theme

Light by design and colours only - font sizing is per-card with card-mod
because HA exposes no dependable global font-size variable."
git push
```

- [ ] **Step 4: Verify the theme is selectable**

After the deploy, in HA → user profile → Themes.
Expected: `family` appears in the dropdown, and selecting it turns the background light grey with blue accents.

---

### Task 5: The dashboard shell and tab bar

**Files:**
- Create: `homeassistant/config/dashboards/family.yaml`
- Create: `homeassistant/config/dashboards/family/_navbar.yaml`
- Create: `homeassistant/config/dashboards/family/home.yaml`
- Create: `homeassistant/config/dashboards/family/calendar.yaml`
- Create: `homeassistant/config/dashboards/family/chores.yaml`
- Create: `homeassistant/config/dashboards/family/kitchen.yaml`
- Create: `homeassistant/config/dashboards/family/more.yaml`
- Modify: `homeassistant/config/configuration.yaml` (`lovelace.dashboards`)

**Interfaces:**
- Consumes: `custom:navbar-card` from Task 3; the `family` theme from Task 4.
- Produces: dashboard at `url_path` `home-family` with view paths `home`, `calendar`, `chores`, `kitchen`, `more`. Phases 3–7 replace each view file's placeholder section with real content and touch nothing else.

- [ ] **Step 1: Create the shell**

Create `homeassistant/config/dashboards/family.yaml`:

```yaml
# Family - the kitchen wall tablet.
#
# This file is the shell only. Each view lives in its own file under
# dashboards/family/ so no single file grows unwieldy, and every view ends with
# the same !include'd navbar - one definition, five uses.
#
# !include resolves relative to the file containing it, so the paths below are
# relative to dashboards/, and the _navbar.yaml includes inside each view file
# are relative to dashboards/family/.
#
# kiosk-mode is deliberately NOT configured here. The tablet loads
#   http://192.168.15.4:8123/home-family/home?kiosk
# and kiosk-mode strips the header and sidebar from that URL parameter alone.
# Configuring it in this file instead would strip the chrome for laptops and
# phones too, on a dashboard the adults also use.

views:
  - !include family/home.yaml
  - !include family/calendar.yaml
  - !include family/chores.yaml
  - !include family/kitchen.yaml
  - !include family/more.yaml
```

- [ ] **Step 2: Create the shared tab bar**

Create `homeassistant/config/dashboards/family/_navbar.yaml`:

```yaml
# The bottom tab bar, included by every view in this dashboard.
#
# navbar-card renders fixed-position, so it floats above the layout regardless
# of which grid section declares it - its placement in a view is bookkeeping,
# not layout.
#
# Five tabs is deliberate: bottom bars degrade badly past six, and every target
# has to survive a four-year-old's aim.
type: custom:navbar-card
desktop:
  position: bottom
  show_labels: true
mobile:
  show_labels: true
routes:
  - url: /home-family/home
    icon: mdi:home-heart
    label: Home
  - url: /home-family/calendar
    icon: mdi:calendar-month
    label: Calendar
  - url: /home-family/chores
    icon: mdi:star-circle
    label: Chores
  - url: /home-family/kitchen
    icon: mdi:silverware-fork-knife
    label: Kitchen
  - url: /home-family/more
    icon: mdi:dots-horizontal
    label: More
```

- [ ] **Step 3: Create the five views**

Create `homeassistant/config/dashboards/family/home.yaml`:

```yaml
# Home - the idle/default view. Built last, in Phase 7, because it summarises
# every other view.
title: Home
path: home
icon: mdi:home-heart
type: sections
max_columns: 2
sections:
  - type: grid
    cards:
      - type: heading
        heading: Home
        heading_style: title
        icon: mdi:home-heart
      - type: markdown
        content: >-
          Phase 7 fills this in: date and weather, today's events, tonight's
          dinner, Aria's remaining chores, and any running timers.
  - type: grid
    cards:
      - !include _navbar.yaml
```

Create `homeassistant/config/dashboards/family/calendar.yaml`:

```yaml
# Calendar - week planner and the add-event composer. Filled in Phase 3.
title: Calendar
path: calendar
icon: mdi:calendar-month
type: sections
max_columns: 2
sections:
  - type: grid
    cards:
      - type: heading
        heading: Calendar
        heading_style: title
        icon: mdi:calendar-month
      - type: markdown
        content: >-
          Phase 3 fills this in: the week planner across the six filtered
          calendar entities, plus the add-event composer that prepends the
          person prefix automatically.
  - type: grid
    cards:
      - !include _navbar.yaml
```

Create `homeassistant/config/dashboards/family/chores.yaml`:

```yaml
# Chores - Aria's picture tiles and star balance. Filled in Phase 4.
title: Chores
path: chores
icon: mdi:star-circle
type: sections
max_columns: 2
sections:
  - type: grid
    cards:
      - type: heading
        heading: Chores
        heading_style: title
        icon: mdi:star-circle
      - type: markdown
        content: >-
          Phase 4 fills this in: Aria's picture chore tiles, her star balance,
          and progress toward the next reward. Eden has no chores.
  - type: grid
    cards:
      - !include _navbar.yaml
```

Create `homeassistant/config/dashboards/family/kitchen.yaml`:

```yaml
# Kitchen - weekly dinners, tonight's recipe, and timers. Filled in Phase 5.
title: Kitchen
path: kitchen
icon: mdi:silverware-fork-knife
type: sections
max_columns: 2
sections:
  - type: grid
    cards:
      - type: heading
        heading: Kitchen
        heading_style: title
        icon: mdi:silverware-fork-knife
      - type: markdown
        content: >-
          Phase 5 fills this in: the week's dinners from Mealie with tonight's
          enlarged, the full recipe on tap, and the kitchen timers.
  - type: grid
    cards:
      - !include _navbar.yaml
```

Create `homeassistant/config/dashboards/family/more.yaml`:

```yaml
# More - lights, climate, map, cameras, garage. The last three are PIN-gated.
# Filled in Phase 6.
title: More
path: more
icon: mdi:dots-horizontal
type: sections
max_columns: 2
sections:
  - type: grid
    cards:
      - type: heading
        heading: More
        heading_style: title
        icon: mdi:dots-horizontal
      - type: markdown
        content: >-
          Phase 6 fills this in: lights and climate open to everyone, then the
          PIN keypad gating the family map, cameras and the garage door.
  - type: grid
    cards:
      - !include _navbar.yaml
```

- [ ] **Step 4: Register the dashboard**

In `homeassistant/config/configuration.yaml`, add to `lovelace.dashboards`, before the `home-admin` entry:

```yaml
    home-family:
      mode: yaml
      filename: dashboards/family.yaml
      title: Family
      icon: mdi:account-group
      show_in_sidebar: true
      require_admin: false
```

The key must contain a hyphen — HA rejects a single-word `url_path`.

- [ ] **Step 5: Validate**

Run: `./tools/check-config.sh`
Expected: `OK: config is valid`, including an `ok` line for `/config/dashboards/family.yaml`.

The harness's second stage exists precisely for this: `check_config` alone does **not** parse YAML-mode dashboards (verified 2026-08-23 — a `!include` pointing at a nonexistent file exits 0 in silence, because HA loads those files lazily when a browser first requests them). A broken include would otherwise surface as a blank dashboard on the kitchen wall rather than as a failed deploy.

- [ ] **Step 6: Commit and push**

```bash
git add homeassistant/config/dashboards/family.yaml \
        homeassistant/config/dashboards/family/ \
        homeassistant/config/configuration.yaml
git commit -m "feat(family): dashboard shell with five-tab bottom navigation

Shell only - one file per view, each including the shared navbar. Phases 3-7
replace the placeholder section in each view and touch nothing else."
git push
```

- [ ] **Step 7: Verify navigation on a laptop**

After the deploy, open `http://192.168.15.4:8123/home-family/home`.
Expected: a `Family` entry in the sidebar; a bottom bar with five labelled tabs; tapping each moves between Home, Calendar, Chores, Kitchen and More; each shows its placeholder text. No `Custom element doesn't exist: navbar-card`.

- [ ] **Step 8: Verify the kiosk URL parameter strips the chrome**

Open `http://192.168.15.4:8123/home-family/home?kiosk`.
Expected: HA's top header and left sidebar are both gone; the navbar-card tab bar remains and still navigates. If the chrome is still present, kiosk-mode's resource is not loading — return to Task 3 Step 6 rather than working around it here.

---

### Task 6: Install MS365-Calendar and ChoreOps

Completes Phase 0. Both integrations are installed and proven to produce entities now, so that Phases 3 and 4 begin with their dependencies already working. Neither is consumed by any dashboard yet.

**Files:** none in this repo — HACS installs into gitignored `custom_components/`, and both are configured through config entries stored in gitignored `.storage/`.

**Interfaces:**
- Produces: six `calendar.family_*` entities (Phase 3 consumes) and ChoreOps entities (Phase 4 consumes).

- [ ] **Step 1: Create the Entra ID app registration**

In the Azure portal for the `pratley.au` tenant, as tenant admin: register a new application, add a client secret, and grant the delegated Microsoft Graph permissions listed in the MS365-Calendar docs at https://rogerselwyn.github.io/MS365-Calendar/. Grant admin consent — self-service, since Nick owns the tenant.

Record the client secret's **expiry date**. When it lapses, calendar sync stops silently; the spec's failure-mode notification covers detection, but the diary entry prevents it.

- [ ] **Step 2: Create the shared Family calendar**

In Outlook as `nick@pratley.au`, create a calendar named `Family` and share it read/write with `elle@pratley.au`. Both accounts are in the same tenant, so this is internal sharing.

- [ ] **Step 3: Install both integrations via HACS**

HACS → Integrations:
- `MS365 Calendar` (RogerSelwyn/MS365-Calendar)
- `ChoreOps` (ccpk1/ChoreOps) — add as a custom repository if it does not appear in the default list.

Restart Home Assistant after installing.

- [ ] **Step 4: Configure MS365-Calendar**

Add the integration in HA, supplying the client ID and secret from Step 1, and complete the OAuth flow. Confirm the `Family` calendar is discovered. Leave the per-person filtering alone — that is Phase 3's work.

- [ ] **Step 5: Configure ChoreOps**

Add the integration and complete its setup wizard. Create one person, `Aria`, and one throwaway chore to prove entities appear. Eden gets nothing. Full chore configuration is Phase 4.

- [ ] **Step 6: Verify entities exist**

```bash
ssh 192.168.15.4 'python3 -c "
import json
d=json.load(open(\"/Users/nickp/ha/homeassistant/config/.storage/core.entity_registry\"))[\"data\"][\"entities\"]
cal=[e[\"entity_id\"] for e in d if e[\"entity_id\"].startswith(\"calendar.\")]
cho=[e[\"entity_id\"] for e in d if \"chore\" in e[\"entity_id\"].lower()]
print(\"calendars:\", cal)
print(\"chores:\", cho)
"'
```

Expected: at least one `calendar.*` entity for the Family calendar, and a non-empty ChoreOps list. Record the exact IDs.

- [ ] **Step 7: Record the MS365 action signature for Phase 3**

In HA → Developer Tools → Actions, search for the MS365 create-event action. Record its exact name and full parameter list into the Phase 3 plan when that plan is written. The spec deliberately does not assume this; confirm it against the installed version.

---

### Task 7: Onboard the tablet with Fully Kiosk

Hardware-gated: needs the Galaxy Tab mounted and powered. Tasks 1–6 do not depend on it and can all complete while the hardware is in transit.

**Files:** none — integration setup only.

**Interfaces:**
- Produces: Fully Kiosk entities under the device name `Kitchen Tablet`. Task 8 consumes `sensor.kitchen_tablet_current_page`, `sensor.kitchen_tablet_battery_level`, `number.kitchen_tablet_screen_brightness`, `switch.kitchen_tablet_screensaver`, and the restart button.

- [ ] **Step 1: Install and license Fully Kiosk**

Install Fully Kiosk Browser from the Play Store on the Galaxy Tab and buy the Plus licence (~$8, one-time).

- [ ] **Step 2: Configure the device**

In Fully Kiosk settings:
- **Start URL:** `http://192.168.15.4:8123/home-family/home?kiosk`
- **Web Content Settings** → enable *Remote Admin* and set a password. The HA integration cannot connect without this.
- **Device Management** → enable *Keep Screen On*.
- **Motion Detection** → enable, and enable *Wake up Screen on Motion*. This is device-side on purpose: it works whether or not the HA integration exposes motion as an entity, so waking on approach never depends on HA being up.
- **Screensaver** → enable, set the screensaver to a photo slideshow. This is the Skylight photo-frame behaviour, free.

- [ ] **Step 3: Add the integration to Home Assistant**

HA → Settings → Devices & Services → Add Integration → Fully Kiosk Browser. Give the tablet's IP and the Remote Admin password. **Name the device exactly `Kitchen Tablet`** — Task 8's entity IDs derive from it.

- [ ] **Step 4: Verify the entity IDs match what Task 8 expects**

```bash
ssh 192.168.15.4 'python3 -c "
import json
d=json.load(open(\"/Users/nickp/ha/homeassistant/config/.storage/core.entity_registry\"))[\"data\"][\"entities\"]
for e in sorted(x[\"entity_id\"] for x in d if \"kitchen_tablet\" in x[\"entity_id\"]): print(e)
"'
```

Expected: entities including a `current_page` sensor, a `battery_level` sensor, a `screen_brightness` number, a `screensaver` switch, and a restart-browser button.

If any ID differs from the names in the Interfaces block above, use the printed name in Task 8 and note the substitution in the commit message. Do not rename entities to fit the plan.

- [ ] **Step 5: Verify HA can drive the screen**

In Developer Tools → States, set `number.kitchen_tablet_screen_brightness` to `20`.
Expected: the tablet visibly dims. Set it back to `200`.

---

### Task 8: Tablet behaviour package

**Files:**
- Create: `homeassistant/config/packages/family_kiosk.yaml`

**Interfaces:**
- Consumes: the Fully Kiosk entities verified in Task 7; `notify.mobile_app_nicks_iphone`.
- Produces: `input_number.kiosk_day_brightness`, `input_number.kiosk_night_brightness`, and five automations prefixed `family_kiosk_`.

- [ ] **Step 1: Write the package**

Create `homeassistant/config/packages/family_kiosk.yaml`:

```yaml
# Kitchen wall tablet behaviour. Makes the Galaxy Tab an appliance rather than a
# browser someone left open.
#
# Waking on approach is NOT here: it is a Fully Kiosk device-side setting, so
# the screen still lights up when someone walks in even if Home Assistant is
# down. Only the things that genuinely need HA live in this file.
#
# Entity IDs assume the Fully Kiosk device was named "Kitchen Tablet" during
# setup - verified in Task 7 Step 4 before this file was written.

input_number:
  kiosk_day_brightness:
    name: Tablet day brightness
    min: 10
    max: 255
    step: 5
    icon: mdi:brightness-7
  kiosk_night_brightness:
    name: Tablet night brightness
    min: 1
    max: 255
    step: 1
    icon: mdi:brightness-3

automation:
  # ===================================================================
  # BRIGHTNESS SCHEDULE
  # ===================================================================
  # Brightness is a helper rather than a literal so it can be tuned from the
  # tablet itself at the time of day it looks wrong, without a deploy.
  - id: family_kiosk_night_dim
    alias: "Kiosk: dim for the night"
    mode: single
    triggers:
      - trigger: time
        at: "21:30:00"
    actions:
      - action: number.set_value
        target:
          entity_id: number.kitchen_tablet_screen_brightness
        data:
          value: "{{ states('input_number.kiosk_night_brightness') | int(8) }}"
      - action: switch.turn_on
        target:
          entity_id: switch.kitchen_tablet_screensaver

  - id: family_kiosk_day_brighten
    alias: "Kiosk: back to day brightness"
    mode: single
    triggers:
      - trigger: time
        at: "06:00:00"
    actions:
      - action: switch.turn_off
        target:
          entity_id: switch.kitchen_tablet_screensaver
      - action: number.set_value
        target:
          entity_id: number.kitchen_tablet_screen_brightness
        data:
          value: "{{ states('input_number.kiosk_day_brightness') | int(200) }}"

  # ===================================================================
  # RETURN HOME WHEN ABANDONED
  # ===================================================================
  # Without this, whoever last used the tablet decides what the kitchen looks
  # at until someone changes it - usually a camera feed, overnight.
  #
  # device_id() derives the target from the entity rather than hardcoding an
  # opaque UUID that would silently rot if the integration were re-added.
  - id: family_kiosk_return_home
    alias: "Kiosk: return to the Home view when abandoned"
    mode: single
    triggers:
      - trigger: state
        entity_id: sensor.kitchen_tablet_current_page
        for: "00:02:00"
    conditions:
      - condition: template
        value_template: >-
          {{ not states('sensor.kitchen_tablet_current_page')
                .endswith('/home-family/home?kiosk') }}
    actions:
      - action: fully_kiosk.load_url
        target:
          device_id: "{{ device_id('sensor.kitchen_tablet_current_page') }}"
        data:
          url: "http://192.168.15.4:8123/home-family/home?kiosk"

  # ===================================================================
  # SELF-RECOVERY
  # ===================================================================
  # A page that never unloads grows its renderer indefinitely. Restart it at an
  # hour nobody is in the kitchen.
  - id: family_kiosk_nightly_restart
    alias: "Kiosk: nightly browser restart"
    mode: single
    triggers:
      - trigger: time
        at: "04:00:00"
    actions:
      - action: button.press
        target:
          entity_id: button.kitchen_tablet_restart_browser

  # ===================================================================
  # WATCHDOG
  # ===================================================================
  # Same pattern as the host-health alerting in packages/extras.yaml: a wall
  # panel that has quietly died looks exactly like one that is merely asleep.
  - id: family_kiosk_offline
    alias: "Kiosk: tablet has been offline for 30 minutes"
    mode: single
    triggers:
      - trigger: state
        entity_id: sensor.kitchen_tablet_battery_level
        to: "unavailable"
        for: "00:30:00"
    actions:
      - action: notify.mobile_app_nicks_iphone
        data:
          title: "Kitchen tablet offline"
          message: >-
            The kitchen tablet has been unreachable for 30 minutes. Check its
            power and that Fully Kiosk is still running.
          data:
            tag: kiosk_watchdog
            group: host
```

- [ ] **Step 2: Set the brightness defaults**

`input_number` helpers have no `initial:` here on purpose — HA restores them across restarts, and an `initial:` would clobber a hand-tuned value on every restart. Set them once after deploy, in Developer Tools → States: day `200`, night `8`.

- [ ] **Step 3: Validate**

Run: `./tools/check-config.sh`
Expected: `OK: config is valid`.

- [ ] **Step 4: Commit and push**

```bash
git add homeassistant/config/packages/family_kiosk.yaml
git commit -m "feat(family): tablet behaviour package for the kitchen kiosk

Brightness schedule, return-home-when-abandoned, nightly browser restart, and
an offline watchdog. Wake-on-motion stays a Fully Kiosk device-side setting so
the screen still wakes when HA is down."
git push
```

- [ ] **Step 5: Verify each automation actually fires**

Do not wait for the clock. In Developer Tools → Actions, run `automation.trigger` against each in turn and watch the tablet:

| Automation | Expected |
|---|---|
| `family_kiosk_night_dim` | Screen dims to the night value; screensaver comes on. |
| `family_kiosk_day_brighten` | Screensaver off; screen returns to the day value. |
| `family_kiosk_return_home` | Navigate the tablet to the More tab first, then trigger. The tablet returns to Home. |
| `family_kiosk_nightly_restart` | Fully Kiosk restarts and reloads the start URL. |

- [ ] **Step 6: Verify the watchdog by actually breaking it**

Turn the tablet off at the wall. Wait 30 minutes.
Expected: a push notification titled "Kitchen tablet offline" on Nick's iPhone. Power it back on and confirm the entities recover.

---

## Phase 0–2 Exit Criteria

- [ ] `./tools/check-config.sh` passes, and is proven to fail on both a YAML error and a schema error.
- [ ] Mealie is running on the mac mini and its entities exist in HA.
- [ ] `navbar-card` and `kiosk-mode` load with no console errors.
- [ ] The `family` theme is selectable.
- [ ] `/home-family/home` shows five working tabs; `?kiosk` strips the HA chrome.
- [ ] The `Family` calendar exists as at least one `calendar.*` entity, and ChoreOps entities exist. (The spec's Phase 0 exit criterion says "all six calendar entities"; that is wrong — the six *filtered* entities are created by Phase 3's `ms365_calendars_*.yaml`. Phase 0 only has to prove the connection works.)
- [ ] The MS365 create-event action's exact name and parameters are recorded for the Phase 3 plan.
- [ ] The tablet dims at night, returns to Home when abandoned, restarts nightly, and alerts when it goes offline.
- [ ] The five views render correctly **at the tablet's own resolution**, not merely in a laptop browser — the spec's Testing section requires this, and Task 5 could only check a laptop if the hardware had not arrived yet. Re-check after Task 7 puts the dashboard on the real screen.

## Known Limitations Leaving This Phase

- **Every view is a placeholder.** That is the point: Phases 3–7 add content to a shell already proven to render, theme and navigate.
- **The PIN gate does not exist yet**, so the More tab must not link to the garage door until Phase 6 lands.
- **`check_config` cannot catch entity-reference errors.** Every later phase must verify its entities against the live registry, not just validate YAML.
- **Mealie is not covered by the GitOps deploy.** Its image tag is pinned in `docker-compose.yml`, but upgrading it requires a manual `docker compose up -d mealie` on the mac mini.
