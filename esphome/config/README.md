# ESPHome configs

**This is the only ESPHome config directory.** `docker-compose.yml` mounts it as the container's
`/config`:

```yaml
esphome:
  volumes:
    - ./esphome/config:/config
```

There used to be a second one at `homeassistant/config/esphome/`, left over from when ESPHome ran
with Home Assistant's config directory as its `/config` — `archive/actron-controller.yaml` still
shows the assumption, referencing `/config/Actron485/components` where `Actron485` lives under
`homeassistant/config/`. Its `.gitignore` dated from 2024-10-28.

Keeping both cost real time: `garage-door.yaml` existed in both copies and had to be edited twice to
stay in sync, and each directory needed its own `secrets.yaml` because ESPHome resolves `!secret`
relative to the config directory. Consolidated 2026-07-31.

## Flashing

**Build from the Mac mini, not the laptop.** The laptop CLI is ESPHome 2025.5.2; the devices run
2026.7.2. OTA from the older client fails with `Error auth: Authentication invalid. Is the password
correct?`, which points at exactly the wrong thing — the password is fine.

```sh
ssh 192.168.15.4
docker run --rm --network host -v ~/ha/esphome/config:/config \
  ghcr.io/esphome/esphome:stable run <device>.yaml --device <ip>
```

Check `sw_version` in HA's device registry against `esphome version` before suspecting credentials.

**Never `scp` into `~/ha`** — it is the GitOps working tree. An untracked file that the repo also
tracks blocks the next `git merge --ff-only`, and the deploy fails with *"untracked working tree
files would be overwritten by merge"*. Everything goes through git.

## Current devices

| config | device | address |
|---|---|---|
| `garage-door.yaml` | Garage Door — relay, BTHome BLE proxy | 192.168.15.17 |
| `actron-sniffer.yaml` | Actron LR7 RS485 sniffer, listen-only | 192.168.15.229 |
| `power-monitor-2-f27486.yaml` | Power Monitor 2 — Lounge Media | 192.168.15.231 |
| `msb-power-monitor.yaml` | not currently deployed | — |

## Known gap: power monitors 1, 3 and 4

Those three plugs are live, but **their current configs are not in this repo**. The running devices
carry MAC-suffixed names — `Power Monitor 1 - Network Rack 575ec1`, `... f257d8`, `... f27381` —
which none of the archived 412-line configs produce. Only power monitor 2 has its short,
remote-package config here, and that is the form that matches what is actually flashed.

They were evidently re-adopted using Athom's package style and only one of the resulting configs was
kept. Nothing is broken — the plugs run fine and report to HA — but **if one needs reflashing there
is no source for it**. Recreate from `power-monitor-2-f27486.yaml`, substituting the device's own
name and MAC suffix, and confirm against the device's entity IDs before flashing.

## archive/

Superseded configs, kept for reference. Not built.

`actron-controller.yaml` in particular must **not** be reused: it writes `0xAA 0xBB 0xCC` onto the
RS485 bus every 5 seconds and uses GPIO5 (a strapping pin) for RX. It also targets a protocol this
aircon does not speak. `actron-sniffer.yaml` replaces it.
