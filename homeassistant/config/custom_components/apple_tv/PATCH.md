# apple_tv — vendored fork of the core integration

This is Home Assistant's own `apple_tv` integration, copied from
`homeassistant/components/apple_tv` at **2026.7.4**, with **one change**.

## The change

Both calls to `pyatv.scan()` no longer pass HA's shared `AsyncZeroconf`
instance:

- `__init__.py` — `AppleTVManager._scan()`
- `config_flow.py` — `_scan()`

## Why

Passing HA's shared `AsyncZeroconf` to `pyatv.scan()` makes it return **zero
devices**, no matter how that instance is configured. Measured inside this
container on 2026-08-29, using HA's own identifier and protocol filters against
the Lounge Apple TV at `192.168.15.13`:

| aiozc passed to `scan()` | devices found |
|---|---|
| none — pyatv makes its own | **1** |
| `InterfaceChoice.Default` + `V4Only` (HA's config) | 0 |
| per-IP `192.168.139.2` | 0 |
| `InterfaceChoice.All` | 0 |

The interface configuration is irrelevant; sharing the instance at all is what
breaks it. Versions: pyatv 0.18.0, zeroconf 0.150.0, HA 2026.7.4.

## What it was costing

`_scan()` returning nothing means the connect loop never succeeds. It retries
with backoff forever, logging only:

```
pyatv.core.scan: 192.168.15.13: Multicast is broken or device offline,
                 trying unicast PTR queries for ['_device-info._tcp.local.']
apple_tv: Failed to find device Lounge Room with address 192.168.15.13
```

The entity then sits at `off` indefinitely. That matters more than it sounds,
because `remote.py` defines availability as:

```python
def is_on(self) -> bool:
    return self.atv is not None
```

A lost connection is reported **identically to a powered-off device**. On
2026-08-29 that caused an automation to treat a dropped connection as "the
Apple TV was switched off" and shut down the TV and soundbar mid-film.

It also caused the config flow to answer `no_devices_found` when adding a
device by name or IP, while a standalone scan found it instantly.

## Maintenance

**This fork shadows a core integration and will go stale.** HA loads
`custom_components/apple_tv` in preference to the built-in one and logs a
warning about it at startup — that warning is expected, not a fault.

On each HA upgrade, check whether upstream still passes `aiozc`. If the
underlying bug is fixed, **delete this directory entirely** and let the core
integration take over again. The `version` field in `manifest.json` records
which core release this was forked from.

Unlike everything else under `custom_components/`, this directory is **tracked
in git** — see the exception in `.gitignore`. It is our patch, not a
HACS download, so it has to survive a fresh checkout.
