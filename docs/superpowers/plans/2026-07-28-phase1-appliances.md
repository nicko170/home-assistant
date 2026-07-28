# Phase 1 — Appliance State Machine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect washing machine and dryer cycles from power draw, record duration and energy per cycle, and notify the phone when each finishes.

**Architecture:** One self-contained HA package (`packages/laundry.yaml`) holding helpers, template sensors and automations. State lives in an `input_select` per appliance so it survives restarts and can be corrected by hand. Thresholds are `input_number` helpers, tunable live without editing YAML.

**Tech Stack:** Home Assistant 2026.7.4 YAML packages, template sensors, `input_select` / `input_number` / `input_datetime` helpers, `notify.mobile_app_*`.

## Global Constraints

- All edits happen in the **laptop clone** `~/code/home-assistant`, then `git push`. The mac mini pulls and deploys within ~2 minutes. Never edit `~/ha` on the mac mini directly — it is pull-only and a dirty tree blocks the `--ff-only` merge.
- Deploy validates with `check_config` before restarting. `check_config` exits 1 only on YAML/parse errors, 0 on runtime errors — a syntactically valid but semantically wrong package WILL deploy.
- Entity IDs are exact and non-obvious. Washer is `sensor.power_monitor_3_f257d8_f257d8_power` (note the doubled `f257d8`). Dryer is `sensor.power_monitor_4_f27381_power`.
- Notify targets that exist: `notify.mobile_app_nicks_iphone`, `notify.mobile_app_iphone`, `notify.mobile_app_nicks_ipad`.
- Secrets never go in this package. The secret gate runs before every commit.

## Threshold Evidence

Derived from **long-term statistics** (hourly mean/min/max, `statistics` table), not raw `states`. The raw 10-day window contains no usable appliance history: the macOS TCP failure meant HA could not reach any device from ~12 July until 15:32 on 28 July, and the recorder's 10-day retention has since rolled past it.

| Measure | Washer | Dryer |
|---|---|---|
| Records / since | 2764 / 2025-09-14 | 2272 / 2025-09-19 |
| Idle hours mean | 0.00 W | 0.00 W |
| Idle hours max | 2.88 W | 0.17 W |
| Active-hour MAX p50 | 1930 W | 666 W |
| Active-hour MEAN p10 | 12 W | 128 W |
| Active-hour MEAN p50 | 87 W | 278 W |
| Cycle length (contiguous active hours) | mostly 2 h | 2–5 h |

Conclusions:

- **Idle is a clean 0 W on both.** A 5 W stop threshold sits far above observed idle noise and far below any real load.
- **Start at 20 W.** Comfortably above idle noise (2.88 W worst case), far below the p10 active-hour max of 222 W / 231 W.
- **Washer needs a long stop debounce.** An active hour can average just 12 W, so cycles contain substantial near-zero stretches (fill, soak, pause). 15 minutes.
- **Dryer is a heat-pump model** (peak ~700–780 W, not the 2–3 kW of a vented dryer) and runs far more continuously (mean p50 278 W). 10 minutes, which also lets the intermittent anti-crease tumble finish before we declare completion.

Hourly statistics cannot resolve sub-minute dips, so the debounce values are informed by the above plus appliance behaviour rather than measured directly. They are exposed as `input_number` helpers precisely so they can be corrected once a real cycle is observed at full resolution — see Task 5.

---

### Task 1: Enable packages and create the laundry package skeleton

**Files:**
- Modify: `homeassistant/config/configuration.yaml`
- Create: `homeassistant/config/packages/laundry.yaml`

- [ ] **Step 1: Enable packages in configuration.yaml**

Add under the existing `homeassistant:` key, or create it if absent. The file currently starts with `default_config:` and has no `homeassistant:` block, so add one at the top:

```yaml
homeassistant:
  packages: !include_dir_named packages
```

- [ ] **Step 2: Create the package with helpers only**

```yaml
# homeassistant/config/packages/laundry.yaml
# Washer/dryer cycle detection. Thresholds are helpers so they can be tuned
# from the UI without editing YAML - see docs in the Phase 1 plan for the
# statistics they were derived from.

input_number:
  washer_start_threshold:
    name: Washer start threshold
    min: 1
    max: 500
    step: 1
    unit_of_measurement: W
    icon: mdi:washing-machine
    initial: 20
  washer_stop_threshold:
    name: Washer stop threshold
    min: 1
    max: 200
    step: 1
    unit_of_measurement: W
    icon: mdi:washing-machine-off
    initial: 5
  washer_stop_minutes:
    name: Washer stop debounce
    min: 1
    max: 60
    step: 1
    unit_of_measurement: min
    icon: mdi:timer-sand
    initial: 15
  dryer_start_threshold:
    name: Dryer start threshold
    min: 1
    max: 500
    step: 1
    unit_of_measurement: W
    icon: mdi:tumble-dryer
    initial: 20
  dryer_stop_threshold:
    name: Dryer stop threshold
    min: 1
    max: 200
    step: 1
    unit_of_measurement: W
    icon: mdi:tumble-dryer-off
    initial: 5
  dryer_stop_minutes:
    name: Dryer stop debounce
    min: 1
    max: 60
    step: 1
    unit_of_measurement: min
    icon: mdi:timer-sand
    initial: 10
  washer_cycle_start_energy:
    name: Washer cycle start energy
    min: 0
    max: 1000000
    step: 0.0001
    unit_of_measurement: kWh
    mode: box
  dryer_cycle_start_energy:
    name: Dryer cycle start energy
    min: 0
    max: 1000000
    step: 0.0001
    unit_of_measurement: kWh
    mode: box

input_datetime:
  washer_cycle_start:
    name: Washer cycle start
    has_date: true
    has_time: true
  dryer_cycle_start:
    name: Dryer cycle start
    has_date: true
    has_time: true

input_select:
  washer_state:
    name: Washer state
    options: [idle, running, finished]
    initial: idle
    icon: mdi:washing-machine
  dryer_state:
    name: Dryer state
    options: [idle, running, finished]
    initial: idle
    icon: mdi:tumble-dryer
```

- [ ] **Step 3: Validate YAML locally before pushing**

```bash
cd ~/code/home-assistant
python3 -c "import yaml,sys; yaml.safe_load(open('homeassistant/config/packages/laundry.yaml')); print('YAML OK')"
```
Expected: `YAML OK`.

- [ ] **Step 4: Commit and let it deploy**

```bash
cd ~/code/home-assistant
git add homeassistant/config/configuration.yaml homeassistant/config/packages/laundry.yaml
git commit -m "feat(laundry): add package with cycle-detection helpers

Thresholds derived from long-term statistics: idle is a clean 0W on both
machines (max noise 2.88W washer, 0.17W dryer), so start=20W and stop=5W
are safe. Washer active hours can average just 12W, so it needs a long
15min stop debounce to survive soak phases; the dryer is a heat-pump model
(peak ~750W) that runs continuously, so 10min suffices."
git push origin main
```

- [ ] **Step 5: Verify the helpers exist after deploy**

Wait ~3 minutes, then:
```bash
ssh 192.168.15.4 'T=$(cat ~/.config/ha-deploy/token); curl -s -H "Authorization: Bearer $T" http://127.0.0.1:8123/api/states | python3 -c "
import json,sys
st=[s[\"entity_id\"] for s in json.load(sys.stdin)]
for p in (\"input_number.washer_\",\"input_number.dryer_\",\"input_select.washer_state\",\"input_select.dryer_state\",\"input_datetime.washer_cycle_start\",\"input_datetime.dryer_cycle_start\"):
    print(p, len([e for e in st if e.startswith(p)]))
"'
```
Expected: non-zero counts for every prefix. If zero, packages did not load — check `configuration.yaml` indentation.

---

### Task 2: Cycle-tracking template sensors

**Files:**
- Modify: `homeassistant/config/packages/laundry.yaml`

**Interfaces:**
- Consumes: `input_datetime.*_cycle_start`, `input_number.*_cycle_start_energy` (Task 1)
- Produces: `sensor.washer_last_cycle_duration`, `sensor.washer_last_cycle_energy`, and dryer equivalents. Task 3's notifications reference these exact entity IDs.

- [ ] **Step 1: Append the template block**

```yaml
template:
  - sensor:
      - name: Washer last cycle duration
        unique_id: washer_last_cycle_duration
        icon: mdi:timer-outline
        state: >
          {% set s = states('input_datetime.washer_cycle_start') %}
          {% if s in ['unknown','unavailable',''] %}
            unknown
          {% else %}
            {% set start = as_timestamp(s) %}
            {% set end = as_timestamp(states.input_select.washer_state.last_changed) %}
            {% set m = ((end - start) / 60) | round(0) %}
            {% if m < 0 %}unknown{% else %}{{ m }}{% endif %}
          {% endif %}
        unit_of_measurement: min

      - name: Washer last cycle energy
        unique_id: washer_last_cycle_energy
        icon: mdi:lightning-bolt
        device_class: energy
        unit_of_measurement: kWh
        state: >
          {% set now_e = states('sensor.power_monitor_3_f257d8_f257d8_total_energy') | float(0) %}
          {% set start_e = states('input_number.washer_cycle_start_energy') | float(0) %}
          {% set d = now_e - start_e %}
          {{ (d if d > 0 else 0) | round(3) }}

      - name: Dryer last cycle duration
        unique_id: dryer_last_cycle_duration
        icon: mdi:timer-outline
        state: >
          {% set s = states('input_datetime.dryer_cycle_start') %}
          {% if s in ['unknown','unavailable',''] %}
            unknown
          {% else %}
            {% set start = as_timestamp(s) %}
            {% set end = as_timestamp(states.input_select.dryer_state.last_changed) %}
            {% set m = ((end - start) / 60) | round(0) %}
            {% if m < 0 %}unknown{% else %}{{ m }}{% endif %}
          {% endif %}
        unit_of_measurement: min

      - name: Dryer last cycle energy
        unique_id: dryer_last_cycle_energy
        icon: mdi:lightning-bolt
        device_class: energy
        unit_of_measurement: kWh
        state: >
          {% set now_e = states('sensor.power_monitor_4_f27381_total_energy') | float(0) %}
          {% set start_e = states('input_number.dryer_cycle_start_energy') | float(0) %}
          {% set d = now_e - start_e %}
          {{ (d if d > 0 else 0) | round(3) }}
```

- [ ] **Step 2: Validate, commit, push**

```bash
cd ~/code/home-assistant
python3 -c "import yaml; yaml.safe_load(open('homeassistant/config/packages/laundry.yaml')); print('YAML OK')"
git commit -am "feat(laundry): add cycle duration and energy template sensors"
git push origin main
```

- [ ] **Step 3: Verify the sensors resolve rather than erroring**

```bash
ssh 192.168.15.4 'T=$(cat ~/.config/ha-deploy/token); for e in sensor.washer_last_cycle_duration sensor.washer_last_cycle_energy sensor.dryer_last_cycle_duration sensor.dryer_last_cycle_energy; do echo -n "  $e = "; curl -s -H "Authorization: Bearer $T" http://127.0.0.1:8123/api/states/$e | python3 -c "import json,sys; print(json.load(sys.stdin).get(\"state\",\"MISSING\"))"; done'
```
Expected: numeric values or `unknown` — **never** `unavailable`, which means a template error. If unavailable, check `home-assistant.log` for the template exception.

---

### Task 3: State machine automations

**Files:**
- Modify: `homeassistant/config/packages/laundry.yaml`

**Interfaces:**
- Consumes: all helpers from Task 1, sensors from Task 2.
- Produces: transitions on `input_select.washer_state` / `input_select.dryer_state`.

- [ ] **Step 1: Append the automations**

```yaml
automation:
  # ---------------- WASHER ----------------
  - id: laundry_washer_start
    alias: "Laundry: washer started"
    mode: single
    triggers:
      - trigger: numeric_state
        entity_id: sensor.power_monitor_3_f257d8_f257d8_power
        above: input_number.washer_start_threshold
        for: "00:01:00"
    conditions:
      - condition: not
        conditions:
          - condition: state
            entity_id: input_select.washer_state
            state: running
    actions:
      - action: input_select.select_option
        target: {entity_id: input_select.washer_state}
        data: {option: running}
      - action: input_datetime.set_datetime
        target: {entity_id: input_datetime.washer_cycle_start}
        data: {timestamp: "{{ now().timestamp() }}"}
      - action: input_number.set_value
        target: {entity_id: input_number.washer_cycle_start_energy}
        data:
          value: "{{ states('sensor.power_monitor_3_f257d8_f257d8_total_energy') | float(0) }}"

  - id: laundry_washer_finished
    alias: "Laundry: washer finished"
    mode: single
    triggers:
      - trigger: numeric_state
        entity_id: sensor.power_monitor_3_f257d8_f257d8_power
        below: input_number.washer_stop_threshold
        for:
          minutes: "{{ states('input_number.washer_stop_minutes') | int(15) }}"
    conditions:
      - condition: state
        entity_id: input_select.washer_state
        state: running
    actions:
      # Compute BEFORE changing state. Template sensors update asynchronously,
      # so reading sensor.washer_last_cycle_* here would race and can report a
      # stale value. These variables are deterministic.
      - variables:
          dur: >-
            {% set s = states('input_datetime.washer_cycle_start') %}
            {% if s in ['unknown','unavailable',''] %}?
            {% else %}{{ ((now().timestamp() - as_timestamp(s)) / 60) | round(0) }}{% endif %}
          kwh: >-
            {{ (((states('sensor.power_monitor_3_f257d8_f257d8_total_energy') | float(0))
                - (states('input_number.washer_cycle_start_energy') | float(0)))
                | round(3)) }}
      - action: input_select.select_option
        target: {entity_id: input_select.washer_state}
        data: {option: finished}
      - action: notify.mobile_app_nicks_iphone
        data:
          title: "Washing machine finished"
          message: "Took {{ dur }} min, used {{ kwh }} kWh."
          data:
            tag: laundry_washer
            group: laundry
      - action: notify.mobile_app_iphone
        data:
          title: "Washing machine finished"
          message: "Took {{ dur }} min, used {{ kwh }} kWh."
          data:
            tag: laundry_washer
            group: laundry

  - id: laundry_washer_reset
    alias: "Laundry: washer back to idle"
    mode: single
    triggers:
      - trigger: state
        entity_id: input_select.washer_state
        to: finished
        for: "06:00:00"
    actions:
      - action: input_select.select_option
        target: {entity_id: input_select.washer_state}
        data: {option: idle}

  # ---------------- DRYER ----------------
  - id: laundry_dryer_start
    alias: "Laundry: dryer started"
    mode: single
    triggers:
      - trigger: numeric_state
        entity_id: sensor.power_monitor_4_f27381_power
        above: input_number.dryer_start_threshold
        for: "00:01:00"
    conditions:
      - condition: not
        conditions:
          - condition: state
            entity_id: input_select.dryer_state
            state: running
    actions:
      - action: input_select.select_option
        target: {entity_id: input_select.dryer_state}
        data: {option: running}
      - action: input_datetime.set_datetime
        target: {entity_id: input_datetime.dryer_cycle_start}
        data: {timestamp: "{{ now().timestamp() }}"}
      - action: input_number.set_value
        target: {entity_id: input_number.dryer_cycle_start_energy}
        data:
          value: "{{ states('sensor.power_monitor_4_f27381_total_energy') | float(0) }}"

  - id: laundry_dryer_finished
    alias: "Laundry: dryer finished"
    mode: single
    triggers:
      - trigger: numeric_state
        entity_id: sensor.power_monitor_4_f27381_power
        below: input_number.dryer_stop_threshold
        for:
          minutes: "{{ states('input_number.dryer_stop_minutes') | int(10) }}"
    conditions:
      - condition: state
        entity_id: input_select.dryer_state
        state: running
    actions:
      # Same reasoning as the washer: compute before the state change.
      - variables:
          dur: >-
            {% set s = states('input_datetime.dryer_cycle_start') %}
            {% if s in ['unknown','unavailable',''] %}?
            {% else %}{{ ((now().timestamp() - as_timestamp(s)) / 60) | round(0) }}{% endif %}
          kwh: >-
            {{ (((states('sensor.power_monitor_4_f27381_total_energy') | float(0))
                - (states('input_number.dryer_cycle_start_energy') | float(0)))
                | round(3)) }}
      - action: input_select.select_option
        target: {entity_id: input_select.dryer_state}
        data: {option: finished}
      - action: notify.mobile_app_nicks_iphone
        data:
          title: "Dryer finished"
          message: "Took {{ dur }} min, used {{ kwh }} kWh."
          data:
            tag: laundry_dryer
            group: laundry
      - action: notify.mobile_app_iphone
        data:
          title: "Dryer finished"
          message: "Took {{ dur }} min, used {{ kwh }} kWh."
          data:
            tag: laundry_dryer
            group: laundry

  - id: laundry_dryer_reset
    alias: "Laundry: dryer back to idle"
    mode: single
    triggers:
      - trigger: state
        entity_id: input_select.dryer_state
        to: finished
        for: "06:00:00"
    actions:
      - action: input_select.select_option
        target: {entity_id: input_select.dryer_state}
        data: {option: idle}
```

- [ ] **Step 2: Validate, commit, push**

```bash
cd ~/code/home-assistant
python3 -c "import yaml; yaml.safe_load(open('homeassistant/config/packages/laundry.yaml')); print('YAML OK')"
git commit -am "feat(laundry): add washer/dryer state machine and notifications"
git push origin main
```

- [ ] **Step 3: Verify all six automations loaded**

```bash
ssh 192.168.15.4 'T=$(cat ~/.config/ha-deploy/token); curl -s -H "Authorization: Bearer $T" http://127.0.0.1:8123/api/states | python3 -c "
import json,sys
a=[s for s in json.load(sys.stdin) if s[\"entity_id\"].startswith(\"automation.laundry\")]
print(\"laundry automations:\", len(a))
for x in sorted(a, key=lambda y:y[\"entity_id\"]): print(\"  \", x[\"entity_id\"], x[\"state\"])
"'
```
Expected: 6 automations, all `on`.

---

### Task 4: Prove the state machine works without waiting for laundry

Do not wait for a real cycle to discover a typo. Drive the transitions directly.

**Files:** none — this is verification only.

- [ ] **Step 1: Force the washer to running and confirm side effects**

```bash
ssh 192.168.15.4 'T=$(cat ~/.config/ha-deploy/token)
curl -s -X POST -H "Authorization: Bearer $T" -H "Content-Type: application/json" \
  -d "{\"entity_id\":\"input_select.washer_state\",\"option\":\"running\"}" \
  http://127.0.0.1:8123/api/services/input_select/select_option >/dev/null
sleep 3
for e in input_select.washer_state sensor.washer_last_cycle_duration sensor.washer_last_cycle_energy; do
  echo -n "  $e = "; curl -s -H "Authorization: Bearer $T" http://127.0.0.1:8123/api/states/$e | python3 -c "import json,sys; print(json.load(sys.stdin)[\"state\"])"
done'
```
Expected: state `running`; duration and energy resolve to numbers or `unknown`, not `unavailable`.

- [ ] **Step 2: Trigger the finished automation manually and confirm the notification**

```bash
ssh 192.168.15.4 'T=$(cat ~/.config/ha-deploy/token)
curl -s -X POST -H "Authorization: Bearer $T" -H "Content-Type: application/json" \
  -d "{\"entity_id\":\"automation.laundry_washer_finished\"}" \
  http://127.0.0.1:8123/api/services/automation/trigger -w "  http %{http_code}\n" -o /dev/null'
```
Expected: HTTP 200, a phone notification reading "Washing machine finished — took N min, used N kWh", and `input_select.washer_state` becomes `finished`.

- [ ] **Step 3: Repeat both steps for the dryer**

Same two calls with `dryer` substituted for `washer` throughout.

- [ ] **Step 4: Reset both to idle**

```bash
ssh 192.168.15.4 'T=$(cat ~/.config/ha-deploy/token)
for a in washer dryer; do
  curl -s -X POST -H "Authorization: Bearer $T" -H "Content-Type: application/json" \
    -d "{\"entity_id\":\"input_select.${a}_state\",\"option\":\"idle\"}" \
    http://127.0.0.1:8123/api/services/input_select/select_option >/dev/null
done; echo reset'
```

---

### Task 5: Capture a real cycle and refine the debounce

The stop-debounce values are the one number that could not be measured directly — hourly statistics cannot resolve sub-minute dips. This task closes that gap once a real load runs.

**Files:**
- Modify: `homeassistant/config/packages/laundry.yaml` (threshold `initial:` values only, if adjustment is needed)

- [ ] **Step 1: After the next real wash, extract the full-resolution power trace**

```bash
ssh 192.168.15.4 'python3 - <<PYEOF
import sqlite3, datetime
DB="/Users/nickp/ha/homeassistant/config/home-assistant_v2.db"
con=sqlite3.connect("file:%s?mode=ro" % DB, uri=True)
rows=con.execute("""SELECT s.state, s.last_updated_ts FROM states s
JOIN states_meta sm ON sm.metadata_id=s.metadata_id
WHERE sm.entity_id="sensor.power_monitor_3_f257d8_f257d8_power"
AND s.last_updated_ts > strftime("%s","now")-86400 ORDER BY s.last_updated_ts""").fetchall()
pts=[(float(v), t) for v,t in rows if v not in ("unknown","unavailable","")]
dips=[]; dip=None
for v,t in pts:
    if v < 5:
        dip = t if dip is None else dip
    else:
        if dip is not None: dips.append(t-dip); dip=None
dips=[d for d in dips if d>30]
print("samples:", len(pts))
if dips:
    dips.sort()
    print("in-cycle dips below 5W longer than 30s: n=%d median=%.0fs max=%.0fs" % (len(dips), dips[len(dips)//2], max(dips)))
    print("-> washer_stop_minutes must exceed %.1f min" % (max(dips)/60.0))
else:
    print("no significant in-cycle dips - current debounce is generous")
PYEOF'
```

- [ ] **Step 2: Adjust if the measured maximum dip approaches the configured debounce**

If the longest in-cycle dip exceeds roughly two thirds of `washer_stop_minutes` (15 min → 10 min), raise the helper in the UI for an immediate fix, then update `initial:` in the package and push so the change survives a restart.

- [ ] **Step 3: Commit any change with the evidence**

```bash
cd ~/code/home-assistant
git commit -am "tune(laundry): adjust washer stop debounce to N min

Measured longest in-cycle dip below 5W was Ns across a real cycle."
git push origin main
```

---

## Phase 1 Exit Criteria

- [ ] `packages/laundry.yaml` deploys cleanly through the GitOps pipeline
- [ ] 6 laundry automations loaded and `on`
- [ ] Both `input_select` state machines transition idle → running → finished
- [ ] Cycle duration and energy sensors resolve to numbers, never `unavailable`
- [ ] A phone notification arrives on manual trigger, with duration and kWh filled in
- [ ] Thresholds are adjustable from the UI without a redeploy

## Known limitations

- Stop-debounce values are informed by hourly statistics plus appliance behaviour, not measured sub-minute dips. Task 5 closes this after the next real cycle.
- Cycle history is "last cycle only". Longer history comes from the recorder and the Phase 2 apexcharts power-signature graphs rather than being stored in helpers.
- The dryer's anti-crease tumble phase draws motor-only power intermittently. With a 5 W stop threshold and 10 min debounce, completion is declared after anti-crease genuinely ends, which may be later than the audible end-of-cycle beep.
