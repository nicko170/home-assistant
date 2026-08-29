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

## Both groups bind, and that took a workaround

`mdns` (224.0.0.251:5353) and `ssdp` (239.255.255.250:1900) both reflect.

Getting there needed two non-obvious things, recorded because they look like
dead ends:

**Receive and send must be separate sockets.** A socket bound to the group
address sends with a source of 224.0.0.251, which is not a valid source
address. The packets leave the host and every receiver silently discards them,
so the reflector looks like it is working while delivering nothing. Send
sockets bind the *interface's* address instead, on the group's port - mDNS
requires responses to originate from 5353 and responders ignore packets that
do not.

**The receive socket cannot go through `net.ListenPacket`.** Go resolves a
multicast listen address to the wildcard internally, so asking for
`239.255.255.250:1900` really attempts `0.0.0.0:1900`, which collides with
OrbStack's wildcard holder. The kernel is happy to bind the group address -
the same call in Python succeeds - so `bind.go` makes the socket with direct
syscalls. mDNSResponder holds 5353 and OrbStack holds 1900, neither sets
`SO_REUSEPORT`, so a wildcard bind can never be shared with them.

Binding an interface's unicast address is not an alternative for receiving:
it binds and joins without error and then receives nothing. Measured at 0
packets over 8s on a busy segment.

## Why `-vm-unicast` is not optional here

Reflected multicast reaches the container's network stack but **not Home
Assistant**, because HA's zeroconf binds `192.168.139.2:5353` - its interface
address - rather than the wildcard. By the same measurement above, such a
socket receives no multicast whatsoever.

So each packet forwarded toward the VM is also sent as a unicast copy directly
to HA's address. mDNS accepts unicast delivery by design (that is what the QU
bit is for), so this is a supported transport rather than a trick.

Without it every layer looks healthy - both groups bound, packets forwarded,
multicast arriving in the container - and HA still discovers nothing. That was
the last blocker, and it is invisible unless you check what HA itself is bound
to. If the container's address changes, update the flag in
`deploy/net.npratley.mdns-reflect.plist`.

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
