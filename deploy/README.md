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
