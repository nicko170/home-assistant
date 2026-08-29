# mdns-reflect

Relays multicast discovery between the house LAN and the OrbStack container
subnet on the mac mini.

## Why

`docker-compose.yml` declares `network_mode: host`. On Linux that would put
Home Assistant on the LAN. On macOS "host" means the **Linux VM's** network
stack, not the mac's, so HA lands on `192.168.138.0/23` behind a NAT:

```
mac mini  en0 192.168.15.4/24        <- the house LAN
          bridge100 192.168.139.3/23 <- OrbStack bridge
HA container         192.168.139.2   <- here
```

Unicast out of the container works (it is NATed), so anything configured by IP
is fine — cameras, ESPHome, Tuya, Plex, Apple TV all connect normally.
Multicast never crosses, so every discovery-based integration is blind.

The mac is on both segments, so it is the only place that can relay. This
binary does that, in both directions.

It replaces `hap-reflect.sh`, which solved one hardcoded slice of the same
problem by republishing HA's single `_hap._tcp` record outbound via `dns-sd`.

## What it does not fix

**UPnP event callbacks.** Sonos, Reolink and SamsungTV subscribe by handing HA's
own address to the device, which then connects *back* by unicast. HA advertises
`192.168.139.2`, which the LAN could not route. That is a routing problem, and
it is fixed elsewhere:

- a static route on the Juniper: `192.168.138.0/23 -> 192.168.15.4`
- `net.inet.ip.forwarding=1` on this host (LaunchDaemon `net.npratley.ip-forward`)

Symptom if that regresses: `Subscription to <ip> failed, attempting to poll
directly` in the HA log, and media players stuck `unavailable`.

## SSDP is best-effort

`mdns` binds and works. `ssdp` usually does **not**, and that is expected.

OrbStack holds a wildcard `*:1900` without `SO_REUSEPORT` (it is forwarding the
container's own SSDP listener), and macOS will not let us share the port —
neither a wildcard nor a group-address bind wins that race. The process logs a
warning and continues with whatever groups it could bind; it only exits if none
bind at all.

The cost is discovery of **new** UPnP devices only. Already-configured ones are
reached by IP and fixed by the route above.

## Run

```
mdns-reflect -lan en0 -vm bridge100 -groups mdns,ssdp
```

| Flag | Default | Meaning |
|---|---|---|
| `-lan` | `en0` | interface on the house LAN |
| `-vm` | `bridge100` | interface on the OrbStack bridge |
| `-groups` | `mdns,ssdp` | groups to reflect |
| `-v` | off | log every forwarded packet |
| `-stats` | `15m` | counter logging interval; `0` disables |

## Install

```
deploy/install-mdns-reflect.sh
```

Builds from this directory, installs the binary to `~/ha-deploy/bin/`, and
loads the LaunchAgent. The binary lives outside the repo for the same reason
`deploy.sh` does: a bad commit must not be able to break it.

## Loop prevention

Three layers, because a reflector that feeds on its own output takes the
network down with it:

1. multicast loopback disabled on the socket
2. packets sourced from any of our own interface addresses are dropped
3. a 2s payload+egress dedupe window

The window is keyed on egress interface so the same packet legitimately
reflected both ways is not mistaken for a loop, and is deliberately short so
genuine repeat announcements still get through.
