# Garage door: ESPHome takeover + Bluetooth restoration — design

**Date:** 2026-07-28
**Device:** `192.168.15.17`, MAC `34:5f:45:aa:7a:58`

## Context

The garage door opener is an ESP32 running **HomeSpan 2.0.0**, paired directly to Apple Home. The
original Arduino sketch has been lost, so the relay GPIO is unknown.

Discovered via mDNS (`_hap._tcp` TXT record):

| Field | Value |
|---|---|
| host | `HomeSpan-282FD0A196AB.local:80` |
| board | `esp32da` — **ESP32** |
| firmware | HomeSpan 2.0.0, Arduino ESP32 core 3.0.7 |
| `ota` | `yes` — ArduinoOTA advertised |
| `ci` | 4 (Garage Door Opener) |
| `sf` | 0 — already paired |
| HAP id | `28:2F:D0:A1:96:AB` |

**The door already has a position sensor.** `binary_sensor.garage_door` is a BTHome BLE sensor
(`5C:53:10:AB:5F:83`) reporting door state, battery, temperature, humidity and voltage. It is
mounted on the door and working. It reads `unavailable` only because HA has no Bluetooth adapter:
the sole adapter was an **ESPHome BLE proxy on the Actron controller ESP32**, which is offline
pending a PCB build. All four BTHome devices are dark for the same reason. The four power monitors
cannot help — they are ESP8266 (`esp8285`) with no Bluetooth radio.

The garage ESP32 sits roughly a metre from the door sensor, so making it the BLE proxy solves door
control and door state with one device.

## Goals

1. Control the door from Home Assistant.
2. Keep phone control through Apple Home.
3. Report true open/closed state.
4. Restore Bluetooth coverage for the garage BTHome sensor.
5. Recover from the lost firmware — the device becomes reproducible, versioned ESPHome config.

## Non-goals

- **Camera vision for door state.** Deliberately cut. A BLE reed sensor already on the door reports
  position reliably; a vision model would do the same job worse — subject to lighting, a parked car
  blocking the view, and inference cost — to solve a problem that does not exist once the radio is
  restored. Revisit only if the sensor proves unreliable in practice, where it would serve as a
  cross-check rather than the primary source.
- Reviving the Aria room and bedroom BTHome sensors. A garage-mounted proxy will probably not reach
  them; they need the Actron back or a second, centrally placed ESP32 proxy.
- Any change to the Actron controller, which is disabled pending its PCB.

## Design

```
ESP32 @ .17  --ESPHome-->  HA (cover + bluetooth_proxy)  --HomeKit Bridge-->  Apple Home
     |                            ^
     +-- relay --> door           +-- BTHome door sensor (BLE, on the door)
```

### Firmware

ESPHome replaces HomeSpan. The device provides:

- a **`cover`** component driven by a **momentary** relay pulse — garage openers take a button
  press, not a held contact
- **`bluetooth_proxy`** so HA regains a Bluetooth adapter
- the standard ESPHome `api` for HA

Position comes from `binary_sensor.garage_door` rather than being inferred from command history, so
the state stays correct if the door is operated by its physical remote.

### Flashing

ArduinoOTA is advertised, so the first attempt is over the air; HomeSpan's OTA password defaults to
`homespan-ota`. USB is the fallback and the device is easily reachable, which makes this low risk.
The first flash is kept deliberately minimal so that a bad config can still be re-flashed over the
air rather than requiring the cable.

### GPIO discovery

The relay pin is unknown and is the one genuine blocker. Approach: flash a minimal ESPHome config
exposing the plausible relay GPIOs as **momentary** switches plus the ESPHome web server. The user
watches the door and toggles pins one at a time until the relay clicks. That pin then goes into the
real config.

This must be done with the door clear and the user present — the discovery step can physically
open the door.

### HomeKit

HA's HomeKit Bridge exposes the cover to Apple Home as a garage door opener. This is the only way
to have both HA and Apple Home control, since a HomeKit accessory pairs to exactly one controller.

### Dashboard

Cover control on Overview beside the garage camera; door state, sensor battery and voltage on
Admin alongside the other health indicators.

## Risks

| Risk | Mitigation |
|---|---|
| Apple Home pairing is destroyed by reflashing | Expected and unavoidable. Existing HomeKit automations or scenes referencing "Garage Door" break and must be re-pointed at the bridged accessory. Flag before flashing. |
| GPIO discovery opens the door unexpectedly | Door clear, user present and watching, momentary switches only. |
| OTA flash fails and bricks the device | USB access confirmed available; first flash kept minimal. |
| Wrong GPIO drives something else on the board | Use momentary pulses only, never a held output, and stop at the first confirmed click. |
| BLE proxy does not reach the door sensor | Unlikely at ~1 m, but if so the sensor is the fallback problem, not the design — a proxy can be relocated. |

## Prerequisites on the user

- Be present for GPIO discovery with the door clear.
- Accept re-adding the garage door to Apple Home after the reflash.
- Confirm the HomeSpan OTA password if it was changed from the default.
