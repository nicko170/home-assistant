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
