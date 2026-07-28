# Garage Door ESPHome Takeover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the lost HomeSpan firmware on the garage ESP32 with ESPHome, giving Home Assistant door control and a Bluetooth proxy, then expose the door back to Apple Home.

**Architecture:** One ESP32 at `192.168.15.17` becomes both the door controller and HA's Bluetooth adapter. Door position comes from the BTHome sensor already mounted on the door, which returns to life as soon as the proxy exists. HA's HomeKit Bridge re-exposes the cover so phone control is unchanged.

**Tech Stack:** ESPHome (ESP32, arduino framework), ArduinoOTA for the one-time firmware replacement, Home Assistant `esphome` + `bluetooth` + `homekit` integrations.

## Global Constraints

- Target device: `192.168.15.17`, MAC `34:5f:45:aa:7a:58`, board **`esp32da`**, currently HomeSpan 2.0.0.
- ESPHome configs for the container live in `~/ha/esphome/config/` on the mac mini (compose mounts it as `/config`). Secrets are in `~/ha/esphome/config/secrets.yaml` — **never inline a secret**, the repo secret gate rejects it.
- Docker binary is `~/.orbstack/bin/docker` and is NOT on the non-interactive PATH. The credential helper needs `PATH="$HOME/.orbstack/bin:$PATH"` for any image pull.
- **ESPHome's OTA cannot flash a HomeSpan device.** The first flash must use ArduinoOTA (`espota.py`) or USB. Every flash after that uses ESPHome's native OTA.
- All repo edits happen in the laptop clone `~/code/home-assistant` and reach the mac mini by `git push`; never edit `~/ha` directly.
- **Tasks 3 and 4 require the user physically present with the garage door clear.** They can open the door.
- Reflashing destroys the Apple Home pairing permanently. Confirm the user is ready before Task 3.

---

### Task 1: Write the discovery firmware config

The relay GPIO is unknown. This config exposes candidate pins as **momentary** switches so the user can find the relay by ear without ever holding an output high.

**Files:**
- Create: `homeassistant/config/esphome/garage-door-discovery.yaml`
- Modify: `homeassistant/config/esphome/secrets.yaml` (on the mac mini only — gitignored)

**Interfaces:**
- Produces: an ESPHome device named `garage-door` reachable at `192.168.15.17`, exposing `switch.garage_pin_<N>` for each candidate GPIO and an ESPHome web server on port 80.

- [ ] **Step 1: Add the API and OTA secrets on the mac mini**

Generate a key and append to the gitignored secrets file:

```bash
ssh 192.168.15.4 'KEY=$(python3 -c "import secrets,base64; print(base64.b64encode(secrets.token_bytes(32)).decode())")
OTAPW=$(python3 -c "import secrets; print(secrets.token_hex(16))")
cat >> ~/ha/homeassistant/config/esphome/secrets.yaml <<EOF

garage_api_key: "$KEY"
garage_ota_password: "$OTAPW"
EOF
cp ~/ha/homeassistant/config/esphome/secrets.yaml ~/ha/esphome/config/secrets.yaml
echo "keys added"'
```

- [ ] **Step 2: Write the discovery config**

Candidate pins exclude GPIO 6–11 (SPI flash, will crash the chip), 34–39 (input only, cannot drive a relay), and the strapping pins 0 and 12 (affect boot mode).

```yaml
# homeassistant/config/esphome/garage-door-discovery.yaml
#
# TEMPORARY firmware. Its only job is to find which GPIO drives the door
# relay, because the original HomeSpan sketch was lost.
#
# Every switch is MOMENTARY (on_turn_on immediately turns itself off after a
# short pulse) so a wrong guess cannot hold an output high. Garage openers
# take a button press, not a held contact.
#
# Excluded: GPIO 6-11 (SPI flash - driving these crashes the chip),
# 34-39 (input only), 0 and 12 (strapping pins that change boot behaviour).

esphome:
  name: garage-door
  friendly_name: Garage Door

esp32:
  board: esp32da
  framework:
    type: arduino

logger:
  level: DEBUG

api:
  encryption:
    key: !secret garage_api_key

ota:
  - platform: esphome
    password: !secret garage_ota_password

wifi:
  ssid: !secret wifi_ssid
  password: !secret wifi_password
  fast_connect: true
  manual_ip:
    static_ip: 192.168.15.17
    gateway: 192.168.15.1
    subnet: 255.255.255.0
    dns1: 192.168.15.1

# Web UI for toggling pins by hand during discovery.
web_server:
  port: 80

switch:
  - platform: gpio
    pin: 4
    name: "Pin 04"
    id: pin_04
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_04
  - platform: gpio
    pin: 5
    name: "Pin 05"
    id: pin_05
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_05
  - platform: gpio
    pin: 13
    name: "Pin 13"
    id: pin_13
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_13
  - platform: gpio
    pin: 14
    name: "Pin 14"
    id: pin_14
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_14
  - platform: gpio
    pin: 16
    name: "Pin 16"
    id: pin_16
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_16
  - platform: gpio
    pin: 17
    name: "Pin 17"
    id: pin_17
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_17
  - platform: gpio
    pin: 18
    name: "Pin 18"
    id: pin_18
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_18
  - platform: gpio
    pin: 19
    name: "Pin 19"
    id: pin_19
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_19
  - platform: gpio
    pin: 21
    name: "Pin 21"
    id: pin_21
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_21
  - platform: gpio
    pin: 22
    name: "Pin 22"
    id: pin_22
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_22
  - platform: gpio
    pin: 23
    name: "Pin 23"
    id: pin_23
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_23
  - platform: gpio
    pin: 25
    name: "Pin 25"
    id: pin_25
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_25
  - platform: gpio
    pin: 26
    name: "Pin 26"
    id: pin_26
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_26
  - platform: gpio
    pin: 27
    name: "Pin 27"
    id: pin_27
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_27
  - platform: gpio
    pin: 32
    name: "Pin 32"
    id: pin_32
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_32
  - platform: gpio
    pin: 33
    name: "Pin 33"
    id: pin_33
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_33
```

- [ ] **Step 3: Validate the YAML parses**

```bash
cd ~/code/home-assistant
python3 -c "import yaml
class L(yaml.SafeLoader): pass
L.add_multi_constructor('!', lambda l,s,n: None)
yaml.load(open('homeassistant/config/esphome/garage-door-discovery.yaml'), Loader=L)
print('YAML OK')"
```
Expected: `YAML OK`.

- [ ] **Step 4: Confirm the secret gate passes and commit**

```bash
ssh 192.168.15.4 'cd ~/ha && python3 tools/secret-gate.py'
cd ~/code/home-assistant
git add homeassistant/config/esphome/garage-door-discovery.yaml
git commit -m "feat(garage): temporary GPIO discovery firmware

The original HomeSpan sketch was lost so the relay pin is unknown. Every
candidate GPIO is exposed as a momentary switch that turns itself off after
400ms - a wrong guess cannot hold an output high. Excludes GPIO 6-11 (SPI
flash), 34-39 (input only) and strapping pins 0 and 12."
git push origin main
```
Expected: gate reports `literals found: 0`.

---

### Task 2: Compile the discovery firmware

**Files:** none — this produces a build artefact on the mac mini.

**Interfaces:**
- Produces: `~/ha/esphome/config/.esphome/build/garage-door/.pioenvs/garage-door/firmware.bin`

- [ ] **Step 1: Copy the config into the ESPHome container's config dir**

The ESPHome container mounts `~/ha/esphome/config` as `/config`, which is a different directory from HA's `homeassistant/config/esphome/`.

```bash
ssh 192.168.15.4 'cp ~/ha/homeassistant/config/esphome/garage-door-discovery.yaml ~/ha/esphome/config/garage-door.yaml && ls -la ~/ha/esphome/config/'
```

- [ ] **Step 2: Compile**

```bash
ssh 192.168.15.4 'export PATH="$HOME/.orbstack/bin:$PATH"
~/.orbstack/bin/docker run --rm --network host \
  -v /Users/nickp/ha/esphome/config:/config \
  ghcr.io/esphome/esphome:stable compile /config/garage-door.yaml 2>&1 | tail -25'
```
Expected: ends with a success line and a path ending `firmware.bin`. First compile downloads the toolchain and takes several minutes.

- [ ] **Step 3: Confirm the binary exists and note its size**

```bash
ssh 192.168.15.4 'find ~/ha/esphome/config/.esphome/build/garage-door -name "firmware.bin" -exec ls -lh {} \;'
```
Expected: a file around 1–2 MB. If absent, re-read the compile output for the error — do not proceed.

---

### Task 3: Flash over ArduinoOTA — USER MUST BE PRESENT

**This destroys the Apple Home pairing permanently.** Confirm the user is ready first.

**Files:** none.

- [ ] **Step 1: Confirm with the user before touching the device**

State plainly: the Apple Home pairing will be lost, any HomeKit automations referencing "Garage Door" will break, and the door will have no control at all until Task 5 completes. Get an explicit go-ahead.

- [ ] **Step 2: Fetch espota.py**

ESPHome's OTA protocol cannot talk to HomeSpan. HomeSpan uses ArduinoOTA, so the first flash needs Arduino's uploader.

```bash
ssh 192.168.15.4 'curl -sL -o /tmp/espota.py https://raw.githubusercontent.com/espressif/arduino-esp32/master/tools/espota.py && head -3 /tmp/espota.py && wc -l /tmp/espota.py'
```
Expected: a Python script of a few hundred lines.

- [ ] **Step 3: Push the firmware**

HomeSpan's default OTA password is `homespan-ota` unless it was changed in the sketch.

```bash
ssh 192.168.15.4 'BIN=$(find ~/ha/esphome/config/.esphome/build/garage-door -name firmware.bin | head -1)
echo "flashing $BIN"
python3 /tmp/espota.py -i 192.168.15.17 -p 3232 -f "$BIN" -a homespan-ota -d -r'
```
Expected: progress dots then `Success`. If it reports an authentication failure the OTA password differs — ask the user, or fall back to USB (Step 5).

- [ ] **Step 4: Confirm the device came back as ESPHome**

```bash
ssh 192.168.15.4 'sleep 45
(dns-sd -B _esphomelib._tcp local > /tmp/e.txt 2>&1 &); sleep 8; pkill dns-sd
grep -i garage /tmp/e.txt || echo "  not advertising yet"
python3 -c "
import socket
s=socket.socket(); s.settimeout(4)
try: s.connect((\"192.168.15.17\", 6053)); print(\"  6053 OPEN - ESPHome API is up\")
except Exception as e: print(\"  6053\", type(e).__name__)
finally: s.close()
"'
```
Expected: `garage-door` in the ESPHome mDNS list and port 6053 open. Port 6053 is the ESPHome API; HomeSpan never listens there, so this is the definitive proof the flash succeeded.

- [ ] **Step 5: USB fallback, only if OTA failed**

Connect the ESP32 by USB to the mac mini, then:

```bash
ssh 192.168.15.4 'ls /dev/cu.usb*'
ssh 192.168.15.4 'export PATH="$HOME/.orbstack/bin:$PATH"
~/.orbstack/bin/docker run --rm --network host --device=/dev/cu.usbserial-0001 \
  -v /Users/nickp/ha/esphome/config:/config \
  ghcr.io/esphome/esphome:stable upload /config/garage-door.yaml --device /dev/cu.usbserial-0001'
```
Substitute the actual device node from the `ls`. If OrbStack cannot pass the USB device through, run `esphome` natively via `pip install esphome` on the mac mini instead.

---

### Task 4: Find the relay GPIO — USER MUST BE PRESENT

**Files:** none — this produces a single number.

- [ ] **Step 1: Confirm the door is clear**

Ask the user to stand where they can see the door, with nothing under it and the car clear. A correct guess will operate the door.

- [ ] **Step 2: Open the ESPHome web UI**

Direct the user to `http://192.168.15.17/` — the discovery firmware serves a page listing `Pin 04` through `Pin 33`.

- [ ] **Step 3: Have the user toggle pins one at a time**

Ask them to press each switch in turn, pausing between, and report which one produces a relay click or moves the door. Suggested order, most-likely first for generic ESP32 relay wiring: 13, 5, 4, 26, 27, 25, 32, 33, 21, 22, 23, 16, 17, 18, 19, 14.

- [ ] **Step 4: Confirm by repeating**

Once a pin is identified, have them press it once more to confirm the same behaviour. Record the number — Task 5 depends on it.

- [ ] **Step 5: If no pin does anything**

The relay may be driven through an inverting driver, in which case the pin idles high and pulses low. Re-flash with `inverted: true` on the GPIO:

```yaml
  - platform: gpio
    pin:
      number: 13
      inverted: true
    name: "Pin 13 inverted"
    id: pin_13
    on_turn_on:
      - delay: 400ms
      - switch.turn_off: pin_13
```

Apply this to the same candidate list and repeat Step 3. If still nothing, the relay is not on a GPIO this firmware can reach and the board needs visual inspection.

---

### Task 5: Final firmware — cover plus Bluetooth proxy

**Files:**
- Create: `homeassistant/config/esphome/garage-door.yaml`
- Delete: `homeassistant/config/esphome/garage-door-discovery.yaml`

**Interfaces:**
- Consumes: the relay GPIO number from Task 4.
- Produces: `cover.garage_door` in HA, and a Bluetooth adapter that restores `binary_sensor.garage_door`.

- [ ] **Step 1: Write the final config**

Replace `RELAY_GPIO` with the number found in Task 4. The cover is a `template` cover so its reported position comes from the real door sensor rather than being inferred from commands — that keeps it correct when the door is operated by its physical remote.

```yaml
# homeassistant/config/esphome/garage-door.yaml
#
# Replaces the lost HomeSpan firmware. This device does two jobs:
#   1. pulses the door relay (momentary, like a button press)
#   2. acts as HA's Bluetooth proxy - it sits about a metre from the BTHome
#      door sensor, which is HA's only source of true open/closed state
#
# Position is read from the BTHome sensor, NOT inferred from commands, so it
# stays correct when the door is operated by the physical remote.

esphome:
  name: garage-door
  friendly_name: Garage Door

esp32:
  board: esp32da
  framework:
    type: arduino

logger:
  level: INFO

api:
  encryption:
    key: !secret garage_api_key

ota:
  - platform: esphome
    password: !secret garage_ota_password

wifi:
  ssid: !secret wifi_ssid
  password: !secret wifi_password
  fast_connect: true
  manual_ip:
    static_ip: 192.168.15.17
    gateway: 192.168.15.1
    subnet: 255.255.255.0
    dns1: 192.168.15.1

# Restores HA's Bluetooth adapter, which was lost when the Actron ESP32 went
# offline. This is what brings binary_sensor.garage_door back to life.
esp32_ble_tracker:
  scan_parameters:
    active: true

bluetooth_proxy:
  active: true

# The physical relay. Never exposed to HA directly - the cover owns it.
switch:
  - platform: gpio
    pin: RELAY_GPIO
    id: relay
    internal: true
    restore_mode: ALWAYS_OFF

# Door position, mirrored from the BTHome sensor in HA.
binary_sensor:
  - platform: homeassistant
    id: door_is_open
    entity_id: binary_sensor.garage_door
    internal: true

cover:
  - platform: template
    name: "Door"
    id: garage_cover
    device_class: garage
    optimistic: false
    assumed_state: false
    lambda: |-
      if (id(door_is_open).state) {
        return COVER_OPEN;
      }
      return COVER_CLOSED;
    open_action:
      - switch.turn_on: relay
      - delay: 400ms
      - switch.turn_off: relay
    close_action:
      - switch.turn_on: relay
      - delay: 400ms
      - switch.turn_off: relay
    stop_action:
      - switch.turn_on: relay
      - delay: 400ms
      - switch.turn_off: relay
```

- [ ] **Step 2: Compile and flash — now over ESPHome's own OTA**

```bash
ssh 192.168.15.4 'cp ~/ha/homeassistant/config/esphome/garage-door.yaml ~/ha/esphome/config/garage-door.yaml
export PATH="$HOME/.orbstack/bin:$PATH"
~/.orbstack/bin/docker run --rm --network host \
  -v /Users/nickp/ha/esphome/config:/config \
  ghcr.io/esphome/esphome:stable run /config/garage-door.yaml --no-logs 2>&1 | tail -15'
```
Expected: `INFO Successfully uploaded program.` No espota this time — the device is running ESPHome now.

- [ ] **Step 3: Remove the discovery config and commit**

```bash
cd ~/code/home-assistant
git rm homeassistant/config/esphome/garage-door-discovery.yaml
git add homeassistant/config/esphome/garage-door.yaml
git commit -m "feat(garage): ESPHome firmware with cover and bluetooth proxy

Relay on GPIO <N>, pulsed momentarily. Cover position is read from the
BTHome door sensor rather than inferred from commands, so it stays correct
when the door is operated by the physical remote.

bluetooth_proxy restores HA's Bluetooth adapter, lost when the Actron ESP32
went offline - this is what brings binary_sensor.garage_door back."
git push origin main
```

---

### Task 6: Verify HA integration and Bluetooth recovery

**Files:** none.

- [ ] **Step 1: Add the ESPHome device to HA**

HA should auto-discover it. If not, add manually:

```bash
ssh 192.168.15.4 'python3 /tmp/add_integration.py esphome 192.168.15.17 6053'
```

- [ ] **Step 2: Confirm the cover exists**

```bash
ssh 192.168.15.4 'T=$(cat ~/.config/ha-deploy/token)
curl -s -H "Authorization: Bearer $T" http://127.0.0.1:8123/api/states | python3 -c "
import json,sys
for s in json.load(sys.stdin):
    if s[\"entity_id\"].startswith(\"cover.\") or \"garage\" in s[\"entity_id\"] and \"door\" in s[\"entity_id\"]:
        print(\"  %-42s %s\" % (s[\"entity_id\"], s[\"state\"]))
"'
```
Expected: a `cover.*` entity for the door.

- [ ] **Step 3: Confirm the BTHome door sensor came back — the key test**

```bash
ssh 192.168.15.4 'T=$(cat ~/.config/ha-deploy/token)
for e in binary_sensor.garage_door sensor.garage_battery sensor.garage_temperature sensor.garage_voltage; do
  printf "  %-38s " $e
  curl -s -H "Authorization: Bearer $T" http://127.0.0.1:8123/api/states/$e | python3 -c "import json,sys; print(json.load(sys.stdin)[\"state\"])"
done'
```
Expected: real values, not `unavailable`. This proves the Bluetooth proxy works. If they are still unavailable after five minutes, check the ESPHome logs for BLE scan activity.

- [ ] **Step 4: Test the door from HA**

With the user watching and the door clear, call the cover service and confirm the door moves and the reported state follows:

```bash
ssh 192.168.15.4 'T=$(cat ~/.config/ha-deploy/token)
curl -s -X POST -H "Authorization: Bearer $T" -H "Content-Type: application/json" \
  -d "{\"entity_id\":\"cover.garage_door_door\"}" \
  http://127.0.0.1:8123/api/services/cover/open_cover -w "  http %{http_code}\n" -o /dev/null'
```
Substitute the actual entity_id from Step 2. Expected: the door operates and `binary_sensor.garage_door` flips within a few seconds.

---

### Task 7: Expose the door back to Apple Home

**Files:**
- Modify: `homeassistant/config/packages/extras.yaml`

- [ ] **Step 1: Add the HomeKit bridge filtered to the cover**

A narrow filter keeps the bridge to just this accessory, so it cannot start
duplicating cameras that Scrypted already bridges.

```yaml
# Appended to homeassistant/config/packages/extras.yaml
#
# Re-exposes the garage door to Apple Home. A HomeKit accessory can only be
# paired to ONE controller, so taking the ESP32 into HA necessarily removed it
# from Apple Home - this bridge puts it back, with HA in the middle.
#
# Filtered to the cover alone. Scrypted already bridges the cameras and we do
# not want two bridges publishing the same accessories.
homekit:
  - name: HA Garage Bridge
    port: 21064
    filter:
      include_entities:
        - cover.garage_door_door
```

Substitute the real cover entity_id from Task 6 Step 2.

- [ ] **Step 2: Deploy and pair**

```bash
cd ~/code/home-assistant
python3 -c "import yaml; yaml.safe_load(open('homeassistant/config/packages/extras.yaml')); print('YAML OK')"
git commit -am "feat(garage): expose door to Apple Home via HomeKit bridge"
git push origin main
```

Then in HA, Settings → Devices & Services → HomeKit Bridge shows a pairing QR code. The user scans it in the Apple Home app. Confirm the door appears and operates from a phone.

- [ ] **Step 3: Tell the user what to fix in Apple Home**

Any pre-existing HomeKit automations or scenes that referenced the old "Garage Door" accessory are broken and must be re-pointed at the newly bridged one. List this explicitly rather than leaving them to discover it.

---

### Task 8: Add the door to the dashboards

**Files:**
- Modify: `homeassistant/config/ui-lovelace.yaml`
- Modify: `homeassistant/config/dashboards/admin.yaml`

- [ ] **Step 1: Add a garage section to Overview**

Insert a new section before the "Needs attention" grid in `ui-lovelace.yaml`:

```yaml
      - type: grid
        cards:
          - type: heading
            heading: Garage
            heading_style: title
            icon: mdi:garage

          - type: tile
            entity: cover.garage_door_door
            name: Garage door
            features:
              - type: cover-open-close

          - type: tile
            entity: binary_sensor.garage_door
            name: Door sensor

          - type: picture-entity
            entity: camera.garage_mainstream
            camera_view: auto
            show_name: false
            show_state: false
            tap_action:
              action: navigate
              navigation_path: /home-cameras/live
```

- [ ] **Step 2: Add sensor health to Admin**

Append to the "Wifi signal" grid's section list in `dashboards/admin.yaml`:

```yaml
          - type: heading
            heading: Garage door sensor
            heading_style: subtitle
            icon: mdi:door
          - type: entities
            show_header_toggle: false
            entities:
              - entity: binary_sensor.garage_door
                name: Door state
              - entity: sensor.garage_battery
                name: Sensor battery
              - entity: sensor.garage_voltage
                name: Sensor voltage
              - entity: sensor.garage_temperature
                name: Garage temperature
```

- [ ] **Step 3: Validate, deploy, verify**

```bash
cd ~/code/home-assistant
for f in ui-lovelace.yaml dashboards/admin.yaml; do
  python3 -c "import yaml; yaml.safe_load(open('homeassistant/config/$f')); print('  $f OK')"
done
git commit -am "feat(dashboards): add garage door control and sensor health"
git push origin main
```

Then confirm both dashboards still return HTTP 200 and the new cards reference live entities.

---

## Exit Criteria

- [ ] `192.168.15.17` runs ESPHome and answers on port 6053
- [ ] A `cover.*` entity operates the door from HA
- [ ] `binary_sensor.garage_door` reports real state, not `unavailable`
- [ ] Cover position follows the door when operated by the physical remote
- [ ] The door appears in Apple Home and works from a phone
- [ ] Door control is on Overview, sensor health on Admin

## Known limitations

- **There is a deliberate circular dependency at boot.** The cover reads its position from
  `binary_sensor.garage_door`, which only has data because this same device is the Bluetooth proxy
  feeding it. On a cold start the cover therefore reports closed until the first BLE advertisement
  arrives and HA pushes the state back. This resolves itself within a minute or so and is
  preferable to the alternative — inferring position from command history, which silently goes
  wrong the first time anyone uses the physical remote.
- The Aria room and bedroom BTHome sensors will most likely stay unavailable — a garage-mounted proxy is unlikely to reach them. They need the Actron back or a second ESP32 proxy placed centrally.
- Cover reports only open/closed, never "opening"/"closing" — the BTHome sensor is a binary reed switch with no intermediate position.
- If the BTHome sensor battery dies the cover will report a stale position. `sensor.garage_battery` is on the Admin dashboard for that reason.
