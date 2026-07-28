# Home Assistant rebuild — design

**Date:** 2026-07-28
**Host:** mac mini `192.168.15.4`, HA in OrbStack container `homeassistant`, config `~/ha/homeassistant/config`
**Current version:** 2026.1.0 → target 2026.4.x

## Context

The existing dashboards are unmaintained and are being discarded. Investigation while fixing an
unrelated outage surfaced several problems that block the rebuild:

- **No camera entities exist in HA at all** (`camera.*` count is 0). The `scrypted` custom component
  crashes on 2026.1 with `TypeError: unhashable type: 'dict'` in `async_register_built_in_panel`.
  That component provides a sidebar *panel*, not camera entities, so fixing it would not deliver
  cameras anyway.
- HA config is **not version controlled** and `secrets.yaml` holds only a placeholder key.
- Several integrations are erroring: Plex (expired token), Roborock (auth), Matter, and an ESPHome
  dashboard connection to a service that isn't running.
- The "Water Front Gardens" automation references a deleted device and is disabled.
- Camera credentials were exposed in a transcript and are being rotated by the user on the evening
  of 2026-07-28.

Hardware available: Sofar G3 inverter (Solarman logger, `.226`), Tuya MSB meter with 16 sub-circuits
(`.227`, currently offline pending wifi rejoin), four ESPHome power monitors (`.230`–`.233`), six
cameras (`.240`–`.245`, one dead), a Reolink PoE doorbell.

## Goals

1. Replace all dashboards with four purpose-built, version-controlled YAML dashboards.
2. Notify on phone when the washing machine or dryer finishes, detected from power draw.
3. Get cameras and the doorbell into HA as first-class entities with usable events.
4. Monitor power properly — whole-home, per-circuit, per-appliance, solar and battery.
5. Leave the system maintainable: versioned, templated, no copy-paste sprawl.

## Non-goals

- Replacing Scrypted. It continues to bridge cameras to Apple HomeKit; only its broken HA
  integration entry is removed.
- Origin Energy API integration. No official or HACS integration exists. Tariff is entered manually
  and structured so a later move to Amber is a config change.
- Repairing camera `.243` (Front Right) — dead hardware, needs physical attention.
- Actron aircon automation — user is building the PCB; deferred.

---

## Phase 0 — Foundations

Nothing else can be built until entities exist. This phase creates them.

### 0.1 Version control and GitOps

`~/ha` is a docker-compose stack directory, not an HA config directory. It defines six services
(`homeassistant`, `nodered`, `esphome`, `scrypted`, `cloudflared`, `watchtower`) but **only
`homeassistant` is actually running** — Scrypted and cloudflared run natively via launchd on the
host instead, so those compose services are stale and should be reviewed separately.

**Repo:** private GitHub repo `nicko170/home-assistant`, repo root = `~/ha`, so the compose file,
HA config and ESPHome device configs are versioned together.

`.gitignore` must exclude service volumes and runtime state: `scrypted/` (875 MB), `nodered/data/`,
`.storage/`, `secrets.yaml` (all locations), `*.db*`, `*.log*`, `backups/`, `.cloud/`, `deps/`,
`tts/`, `www/community/`, `custom_components/` (HACS-managed).

#### 0.1.1 BLOCKER — sanitise ESPHome secrets before the first commit

Nine ESPHome YAML files contain **plaintext wifi SSID, wifi password, OTA passwords and API
encryption keys**: `power-monitor-1/2/3/4.yaml`, `msb-power-monitor.yaml`,
`actron-controller.yaml`, `archive/garden-water.yaml`, `archive/esphome-web-338b00.yaml`,
`esphome/config/power-monitor-2-f27486.yaml`, `archive/power-monitor-3-f257d8-f257d8.yaml`.

These must be converted to `!secret` references **before the first commit**. The pattern already
exists in the codebase — `power-monitor-2-f27486.yaml` uses `!secret` for wifi credentials — it was
simply applied inconsistently. Per-device API keys get individual secret keys
(e.g. `pm1_api_key`).

No commits have been made yet, so there is no history to scrub. This ordering must be preserved:
sanitise → verify → first commit → push. Once secrets enter git history they are permanent.

Verification gate before pushing: re-run the secret scan and confirm zero literals, and confirm the
staged file list contains no `secrets.yaml`, `.storage`, database or log files.

#### 0.1.2 Deploy pipeline

Laptop clone at `~/code/home-assistant` for editing; mac mini `~/ha` is the deploy target.

- **Auth:** read-only **deploy key** on the mac mini. It only pulls; all commits originate from the
  laptop. `gh` is not installed on the mac mini and is not needed. A leaked read-only deploy key
  cannot write to the repo.
- **Trigger:** launchd agent polling every ~2 minutes. Requires no inbound exposure. A GitHub
  webhook through the existing cloudflared tunnel would give instant deploys and can be added
  later; polling is the robust default.
- **Apply, with validation before reload:**
  1. record current `HEAD`
  2. `git pull --ff-only`
  3. validate: `check_config` inside the `homeassistant` container
  4. on failure — `git reset --hard <previous HEAD>` and notify; HA is never left broken
  5. on success — targeted reload, or container restart if `configuration.yaml` changed
- Deploy outcomes (success and failure) notify the mobile app, so a failed deploy is visible rather
  than silent.

### 0.2 Upgrade 2026.1.0 → 2026.4.x

Tar the whole config directory first. Pull the image and restart via the existing compose file.

**Risk:** every custom component here is unofficial (`solarman`, `tuya_local`, `frigate`,
`samsungtv_smart`, `hacs`, `scrypted`) and major upgrades break them — `scrypted` already is broken.
After upgrade, verify each loads. **The inverter (`solarman`) is the critical one**; if it fails,
roll back to 2026.1.0 from the tar and re-plan the upgrade separately.

Rationale for upgrading first: section background colours (2026.4) and sticky footer cards (2026.3)
are used by the Phase 2 dashboard designs.

### 0.3 Cameras as native integrations

Remove the broken `scrypted` config entry. Add instead:

| Camera | IP | Integration | Key entities gained |
|---|---|---|---|
| Reolink Video Doorbell PoE | .240 | `reolink` | doorbell press, person/vehicle detection, chime, two-way audio |
| Reolink CX820 | .245 | `reolink` | person/vehicle detection, stream |
| Hikvision KEPLER-2332 "Front Left" | .241 | `onvif` | motion events, stream |
| Hikvision KEPLER-2332 "Garage" | .242 | `onvif` | motion events, stream |
| Hikvision DS-2CD2732F-IS | .244 | `onvif` | motion events, stream |
| Hikvision "Front Right" | .243 | — | dead hardware, skip |

Credentials go in `secrets.yaml` (keys `camera_username`, `camera_password`) so the user's rotation
that evening is a single-file edit. Hikvision firmware is V5.4.800 (old); ONVIF may need enabling
on the camera itself — verify per camera rather than assuming.

Cameras are statically addressed on-device at `.240`–`.245`, now outside the DHCP pool, so no
reservations are needed.

### 0.4 Energy Dashboard configuration

Configure HA's **built-in** Energy Dashboard rather than recreating it by hand. Use the
`sensor.inverter_total_*` series, which carry `state_class: total_increasing`; the `today_*`
equivalents reset daily and behave incorrectly as energy dashboard sources. Verify `state_class` on
each sensor before wiring it.

- Grid consumption → `sensor.inverter_total_energy_import`
- Return to grid → `sensor.inverter_total_energy_export`
- Solar production → `sensor.inverter_total_production`
- Battery in / out → `sensor.inverter_total_battery_charge` / `..._discharge`
- Individual devices → MSB `sensor.msb_meter_energy_sub01..16`, plus each power monitor's
  `total_energy`

**Tariff:** Origin, entered manually with **placeholder rates** for now; the user will supply real
c/kWh figures shortly. Structured so the numbers are a single edit, and so a later move to Amber is
a config change rather than a rebuild.

**Feed-in tariff is 0.** This is a defining constraint, not a detail — it is the reason the battery
exists. Every exported kWh is given away for nothing, so the system's goal is to export as little
as possible.

Consequences carried through the rest of this design:

- **Export is waste, not income.** The Energy views must present exported energy as loss, never as
  earnings. Its true cost is `exported kWh × import rate` — energy given away that later has to be
  bought back.
- **Self-consumption is the primary KPI**, defined as `(production − export) / production`. It
  should be the headline energy metric on the Overview dashboard.
- **Load shifting into surplus is the highest-value automation available** — see Phase 3. Running a
  load on surplus that would otherwise be exported at $0 is a direct saving at the full import rate.
### 0.4.1 Inverter control surface (verified against the register profile)

The configured profile is `sofar_g3.yaml`. Inspecting it directly, it exposes exactly **three**
writable controls — confirmed against the entity registry:

| Control | Entity | Values |
|---|---|---|
| Export Surplus Limitation | `select.inverter_export_surplus_limitation` | `Disabled` *(current)* / `Enabled` / `Balanced` |
| Export Surplus Power | `number.inverter_export_surplus_power` | watts, scale 100 |
| Storage Control Mode | `select.inverter_storage_control_mode` | `Self Use` *(current)*, `Time of Use`, `Optimized Revenue`, `Passive`, `Peak Shaving`, `Off-Grid`, `Generator`, `Export Priority` |

**Available immediately:**

- **Zero-export enforcement** — set Limitation `Enabled` and Power `0` to force all generation into
  the battery and house loads. With FIT = 0 this is the correct default posture, exposed as a
  dashboard toggle.
- **Grid-charge window control** — `Time of Use` is selectable today. The user's target is a
  free-power window (planned 11:00–15:00 on a future energy plan). The charge *schedule* itself
  lives in registers `0x1113`/`0x1114`, which `sofar_g3.yaml` does **not** map.

**Approach A (adopt now):** configure the 11:00–15:00 charge window once in the SolarMan app, then
have HA toggle Storage Control Mode between `Self Use` and `Time of Use`. This delivers the
requested on/off control with no profile change and no write risk.

**Approach B (evaluate separately):** switch to `sofar_g3hyd.yaml`, which maps
`Timed Charge Start`/`End` (`0x1113`/`0x1114`), `Timed Charge Power` (`0x1117`/`0x1118`),
`Timed Control` (`0x1112`) and timed discharge, making the window fully HA-controlled.

**Approach B is gated on confirming the physical inverter model.** HA reports model "G3" only
because the profile declares it; that is not evidence of the hardware. Writing incorrect registers
to a battery inverter is a serious failure mode — misreads are harmless, miswrites are not. If
adopted, B must happen in Phase 0 *before* dashboards are built, because changing profiles renames
entities.

**Forecast-gated grid charging** (Phase 3, depends on the above): using the existing
`forecast_solar` integration, if tomorrow's forecast production will not refill the battery, switch
to `Time of Use` so it charges during the free window; otherwise remain on `Self Use` and let solar
do it. `Export Priority` mode must never be selected while FIT = 0.

Do not change any inverter setting before self-consumption measurement exists to judge the effect.

### 0.5 Cleanup

- Delete the Garden Water Timer ESPHome entry and the "Water Front Gardens" automation.
- Disable the Actron Controller entry until the PCB is ready, so it stops erroring.
- Surface for the user to action: Plex reauth, Roborock auth, Matter error.
- Track the MSB meter (`.227`) rejoining wifi; it may need a power cycle.

---

## Phase 1 — Appliance state machine

### Source entities

| Appliance | Power | Cumulative energy | Plug |
|---|---|---|---|
| Washing machine | `sensor.power_monitor_3_f257d8_f257d8_power` | `..._total_energy` | `switch.power_monitor_3_f257d8_f257d8_switch` |
| Dryer | `sensor.power_monitor_4_f27381_power` | `..._total_energy` | `switch.power_monitor_4_f27381_switch` |

### Threshold derivation

Thresholds are **derived from recorder history, not guessed**. `home-assistant_v2.db` holds months
of real power data for both appliances. Before writing automations, query the distribution of power
values per appliance to pick start/stop thresholds and a stop debounce that survives mid-cycle idle
phases. Document the chosen numbers and the evidence in the implementation plan.

### State model

Per appliance, an `input_select` with options `idle` / `running` / `finished`. An `input_select`
rather than a template sensor so state survives restarts and can be corrected by hand when tuning.

Transitions:

- **idle → running** — power above start threshold for the start debounce.
- **running → finished** — power below stop threshold *continuously* for the stop debounce. The
  continuous requirement is what prevents soak and pause phases from firing a false finish.
- **finished → running** — power rises above start threshold again (next load).
- **finished → idle** — after a timeout, so a finished state doesn't persist indefinitely.

### Cycle history

On `running` entry, record start time (`input_datetime`) and starting cumulative energy
(`input_number`). On `finished` entry, compute duration and energy delta into template sensors
`sensor.<appliance>_last_cycle_duration` and `sensor.<appliance>_last_cycle_energy`. Long-term
history comes from the recorder; power-signature graphs use `apexcharts-card`.

### Notification

Notify `notify.mobile_app_nicks_iphone` and `notify.mobile_app_iphone` on finish, including cycle
duration and kWh. Laundry notifications fire regardless of presence — you want to know either way.
Presence-awareness applies to camera alerts (Phase 3), not here.

---

## Phase 2 — Dashboards

All four in **YAML mode**, defined under `~/ha/homeassistant/config/dashboards/` and referenced from
`configuration.yaml`. Trade-off accepted by the user: no in-UI editing, in exchange for
reproducibility and diffability.

### Disposition of existing dashboards

All existing dashboards are discarded, as requested. Their `.storage` files are committed to git
before deletion so nothing is lost irrecoverably.

| Existing | Action |
|---|---|
| `lovelace` (default, 548 B) | Replaced by the new YAML **Overview** |
| `dashboard-energy` (38 KB, unmaintained) | Deleted; replaced by built-in Energy Dashboard + new YAML **Energy** |
| `dashboard-test` (393 B) | Deleted |
| `map` (154 B) | Kept — it is HA's native map, costs nothing |

### Card changes

Add via HACS: `sankey-chart-card`, `bubble-card`, `decluttering-card`, `auto-entities`.
Remove: `layout-card`, `stack-in-card`, `bar-card` — superseded by native sections views.
Keep: `power-flow-card-plus`, `advanced-camera-card`, `apexcharts-card`, `mushroom`, `card-mod`,
`mini-graph-card`, `template-entity-row`, `PlexMeetsHomeAssistant`.

All views use the native **sections** layout. Repetition (four power monitors, five cameras) is
handled with `decluttering-card` templates, not copy-paste.

### Overview

At-a-glance home state. Presence badges (Nick, Elle), current house power, battery SoC, solar now,
and **self-consumption %** as the headline energy metric (see 0.4 — feed-in tariff is 0).
`power-flow-card-plus` as the hero. Laundry status with last-cycle summary. Doorbell thumbnail.
An alerts section listing anything unavailable or faulted, built with `auto-entities` so new
problems appear without editing the dashboard.

### Energy

`sankey-chart-card` showing grid + solar → house → the 16 MSB sub-circuits — the main reason for
adding it, since nothing currently visualises those circuits. Battery detail (SoC, power, SoH,
cycles, time-to-empty). Per-circuit table via `auto-entities` + `template-entity-row`. Inverter
health: temperatures, per-phase voltages, fault state. Complements the built-in Energy Dashboard
rather than duplicating it.

### Cameras

`advanced-camera-card` grid over the five live cameras, one `decluttering-card` template reused per
camera. Doorbell given prominence with recent events. Motion and person event timeline.

### Admin

Infra health and the things that actually broke. HA and container status, inverter connection, wifi
signal per ESPHome device, MSB meter reachability, integration errors, pending updates.

---

## Phase 3 — Extras

- **BOM weather + rain radar** — Bureau of Meteorology integration and a radar card, useful next to
  solar forecasting.
- **Actionable doorbell notifications** — press sends a snapshot with action buttons.
- **Solar-surplus laundry — the highest-value item in this design.** Because the feed-in tariff is
  0, surplus being exported is worth nothing, while the same energy consumed on-site saves the full
  import rate. When the battery is full (or near full) and the system is exporting above a
  threshold, run the dryer via `switch.power_monitor_4_f27381_switch`.

  Start conservative: **notify with an actionable "run it now" button** rather than switching
  automatically. Appliances starting unattended is a safety and household-annoyance question, and
  the dryer should not start because a cloud passed. Promote to fully automatic only once the
  surplus signal has been observed to be stable, and always gate on a minimum sustained surplus
  duration rather than an instantaneous reading. Requires Phases 0–1 complete.
- **Presence-aware camera alerts** — suppress motion spam when home.
- **Mac mini health sensor** — push uptime and TIME_WAIT count from the Mac into HA and alert at
  ~45 days uptime, ahead of the macOS 49.7-day TCP clock overflow that caused the outage on
  2026-07-28. Turns that failure mode into a dashboard tile instead of a surprise.

---

## Risks and rollback

| Risk | Mitigation |
|---|---|
| Upgrade breaks `solarman` and the inverter disappears again | Tar config before upgrading; roll back to 2026.1.0 and re-plan if it fails |
| Hikvision cameras don't support ONVIF on V5.4.800 firmware | Verify per camera; fall back to `generic_camera` with RTSP for stream-only, accepting no motion events |
| Appliance thresholds mis-tuned, causing false or missed alerts | Derive from recorder history; expose thresholds as `input_number` helpers so they can be tuned live without editing YAML |
| YAML dashboards lock the user out of UI editing | Accepted explicitly; git history makes changes safe and reversible |
| Credential rotation breaks camera integrations | Credentials live only in `secrets.yaml`; rotation is a one-file edit plus reload |
| Plaintext wifi/API secrets pushed to GitHub | Sanitise to `!secret` **before** the first commit; verification gate re-scans and checks the staged file list before any push |
| A bad commit breaks HA remotely | Deploy validates config before reloading and rolls back to the previous commit on failure; notifies either way |
| Deploy key compromised | Read-only key, scoped to this repo; cannot push |

## Prerequisites on the user

- Rotate camera credentials (planned for the evening of 2026-07-28) and update `secrets.yaml`.
- Plex and Roborock reauthentication.
- Power-cycle the MSB meter if it does not rejoin wifi.
- Origin tariff rates — **placeholder values used for now**; real c/kWh to follow. Feed-in tariff is
  confirmed **0**.
- **Physical inverter model number** (from the inverter label or the SolarMan app) — required before
  Approach B in 0.4.1 can be considered.
- Configure the 11:00–15:00 charge window in the SolarMan app if Approach A is taken.
- Confirm whether the free-power energy plan has been switched to, and its exact window.
