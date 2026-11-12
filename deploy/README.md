# GitOps deploy

Push to `main` from this repo; the mac mini (`192.168.15.4`) picks it up within ~2 minutes.

```
laptop ~/code/home-assistant --push--> github nicko170/home-assistant (private)
                                              |
                                              | pull (read-only deploy key)
                                              v
                                    mac mini ~/ha  --> validate --> restart HA
```

## Safety

`deploy.sh` validates with `check_config` **inside the container** before restarting HA.
If validation fails it hard-resets to the previous commit — a bad push cannot take HA down.

Verified behaviour of `check_config` (2026-07-28):

| Condition | Exit | Deploy action |
|---|---|---|
| YAML/parse error | 1 | roll back, notify, record SHA |
| Runtime component error (e.g. a broken automation) | 0 | proceed |

A failed commit SHA is recorded in `~/ha-deploy/last-failed-sha`. While `origin/main` still
points at it the deployer skips silently, so a broken push does not notify every 2 minutes.
The file is removed on the next successful deploy.

## Layout

- Live script: `~/ha-deploy/deploy.sh` — deliberately **outside** the repo, so a bad commit
  cannot break the deployer itself. The copy here is reference only.
- Agent: `~/Library/LaunchAgents/net.npratley.ha-deploy.plist`, `StartInterval` 120s.
- Log: `~/ha-deploy/deploy.log`.
- Notification token: `~/.config/ha-deploy/token` (mode 600). If absent, notifications are
  skipped and deploys still work.

## The mac mini cannot push

Its deploy key is read-only by design. All commits originate from the laptop clone.


## Container networking (added 2026-08-29)

HA runs in OrbStack. `network_mode: host` in `docker-compose.yml` means the
**Linux VM's** host network, not the mac's — HA sits at `192.168.139.2` on
`192.168.138.0/23`, not on the house LAN. Unicast out is NATed and works, so
IP-configured integrations are fine. Multicast does not cross, and until
2026-08-29 nothing routed back in.

Three pieces make discovery and event callbacks work. All three are required;
any one missing reproduces the original symptoms.

| Piece | Where | Fixes |
|---|---|---|
| static route `192.168.138.0/23 -> 192.168.15.4` | Juniper SRX320 (`192.168.15.1`) | LAN can address the container |
| `net.inet.ip.forwarding=1` | `deploy/install-ip-forward.sh` (LaunchDaemon, needs sudo) | this host actually forwards those packets |
| pf source-NAT to `bridge100` | `deploy/install-pf-ha-container.sh` (needs sudo) | container replies on-link instead of via OrbStack's NAT |
| mDNS reflection | `deploy/install-mdns-reflect.sh` (LaunchAgent) | best-effort multicast discovery |
| explicit device IPs | `packages/media.yaml`, Cast options | discovery bypassed entirely where it still does not work |

The pf piece is the one that actually made it work. Route plus forwarding got
packets *in*, but the container's default route is OrbStack's NAT gateway, so
replies to an off-link LAN host were SNATed to `192.168.15.4` — the client had
opened its connection to `192.168.139.2`, so it dropped them. Source-NATing
inbound traffic to `bridge100`'s address makes the container see an on-link
peer and answer directly.

**Verified 2026-08-29:** `nc -z 192.168.139.2 8123` from a LAN host succeeds,
Sonos went `unavailable` -> `idle`, the Cast soundbar `unavailable` -> `off`.

**Discovery works** as of 2026-08-29. The container's zeroconf sees every
device on the LAN - both Apple TVs, the Sonos, the soundbar, the Frame. Both
mDNS and SSDP reflect.

The first cut of the reflector appeared to forward packets and delivered
nothing: its send socket was bound to the multicast group, so packets went out
with a source of `224.0.0.251` and every receiver dropped them. Send and
receive are now separate sockets. See `tools/mdns-reflect/README.md`.

Sonos and Cast remain pinned by IP (`packages/media.yaml` and the Cast options
flow) as belt-and-braces. Discovery finds them now, but the explicit hosts cost
nothing and mean a reflector regression does not take the speakers with it.

**Symptoms if one regresses:** media players stuck `unavailable`; HA log shows
`Subscription to <ip> failed, attempting to poll directly` (Sonos),
`lost event subscription` (Reolink), or `Upnp services are not available`
(SamsungTV). Check the three above before suspecting hardware — all of these
looked like broken devices for months and none of them were.

SSDP reflection is **expected to fail** and logs a warning: OrbStack holds a
wildcard `*:1900` without `SO_REUSEPORT`. Only discovery of *new* UPnP devices
is affected. See `tools/mdns-reflect/README.md`.

## LAN address plan (added 2026-11-12)

The SRX320 **forgets every DHCP lease when it reboots, and whenever jdhcpd restarts.** That happened
on 2026-11-07 and again on 2026-11-08. Binding persistence (`persistent-storage`) parses on this
box, but it is a hidden statement: Juniper documents it for ACX only. It has never been tried
here, and nothing should depend on it. Anything HA pins by IP must
therefore have an SRX `host` reservation **outside** the dynamic range. Otherwise the next outage
reshuffles it.

| range | use |
|---|---|
| `.1`–`.9` | infrastructure, static (SRX `.1`, sw01 `.2`, mac mini `.4`, cpu-dev-01 `.5`) |
| `.10`–`.99` | SRX reservations: IoT, AV and appliances at the address each already had |
| `.100`–`.225` | **dynamic pool** (phones, laptops, guests) |
| `.226`–`.239` | SRX reservations: ESPHome, Tuya, inverter, Sonos, soundbar, garage door, a40 BMC `.234` |
| `.240`–`.249` | cameras, static IPs configured on the cameras |

The SRX config (`groups Home-Net-001 access address-assignment pool Home-Net-001 family inet
host ...`) is the authoritative list. NetBox does not model the house LAN.

The 2026-11-08 outage moved the soundbar onto the garage door ESP32's hardcoded `.17`. The
garage cover and every BTHome sensor were down for four days, and the Frame's wake-on-LAN had
been pointing at an empty address for weeks.

Adding a reservation for a device that holds a different address needs both steps below. A
binding that is still live keeps getting ACKed at the old address.
1. `clear dhcp server binding <old-ip>`
2. make the client re-request: power-cycle it, or a remote reboot where one exists

The server NAKs the client's INIT-REBOOT and offers the reservation, as seen in `srx-dhcp.log`.
Changing the pool range drops every dynamic binding, but clients keep their address until
renewal, so nothing breaks.
