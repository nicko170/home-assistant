# Family dashboard — kitchen wall tablet — design

**Date:** 2026-08-23
**Target device:** Samsung Galaxy Tab, wall-mounted in the kitchen, always on
**Family:** Nick, Elle, Aria (4), Eden (1)
**Inspiration:** commercial Skylight Calendar; DIY thread
https://community.home-assistant.io/t/diy-family-calendar-skylight/844830 and its source repo
https://github.com/mohesles/my-skylight-calendar (Google Calendar based — we substitute Outlook).

## Context

The existing dashboards (Overview, Power, Cameras, Admin) are built for one admin user on a phone
or laptop. They assume a reader who can parse a sensor name, and they assume a mouse-sized tap
target. None of that survives contact with a 4-year-old standing in a kitchen.

This is a different product sharing the same HA instance: a permanently-on appliance whose primary
users are a 4-year-old, a 1-year-old's parents, and whoever is cooking. It must be legible from
across the room, colourful enough that Aria wants to use it, and safe enough that she cannot open
the garage door.

Constraints inherited from this repo:

- Lovelace is `mode: yaml`. Every HACS frontend card must be declared in `lovelace.resources` in
  `configuration.yaml` or it silently fails to render on **every** dashboard, not just the new one.
- Deploys are GitOps: push to `main`, the mac mini pulls within ~2 min, runs `check_config` inside
  the container, and hard-resets to the previous commit on a parse error. Runtime errors (a broken
  automation, an unavailable entity) exit 0 and deploy anyway — so YAML validity is enforced for us,
  but *entity correctness is not*. Phases must be ordered so entities exist before cards reference
  them.
- Features are packaged: helpers + templates + automations bundled per feature in `packages/`,
  never scattered.
- Secrets go through `!secret`; `tools/secret-gate.py` fails the commit otherwise.

## Goals

1. A permanently-on kitchen dashboard with bottom tab-bar navigation, usable by a 4-year-old.
2. Shared family calendar that round-trips with Outlook/O365, so Nick and Elle see and edit the same
   events from their phones.
3. Picture-based chores for Aria with points, rewards, and parent approval.
4. Weekly dinner plan visible at a glance, with the recipe readable while cooking.
5. Kitchen timers, started in one tap.
6. Cameras and family locations reachable from the tablet, behind a PIN.
7. The tablet behaves like an appliance: wakes on approach, dims at night, recovers itself, and
   returns to the home view when abandoned.

## Non-goals

- **Per-person Outlook calendars.** Decided: one shared `Family` calendar. Per-person colouring is
  achieved by subject prefix + filtered HA calendar entities (see Design § Calendar).
- **Replacing the existing dashboards.** Power, Cameras and Admin stay exactly as they are. The
  family dashboard links out to Cameras rather than reimplementing it.
- **A bespoke web frontend.** Rejected in brainstorming: it discards the HA card ecosystem and
  creates a permanent maintenance burden.
- **Chores for Eden.** She is 1.
- **Adopting ChoreOps' shipped storage-mode dashboard.** It lives in `.storage`, which is
  gitignored, so it would be the one dashboard not under version control. Retained as a named
  fallback if the hand-built chores view proves disproportionate — see Risks.
- **Voice assistant / intercom.** The tablet has a mic and speaker; not in scope.
- **Per-kid HA user accounts.** HA's non-admin permissions are too coarse to be worth the
  user-switching friction on a shared panel.

---

## Design

### Layer split

Integrations own data. Packages own behaviour. Dashboard files own presentation only, and contain
no logic — consistent with `packages/laundry.yaml` and `packages/garage.yaml`.

```
Outlook "Family" calendar ──MS365 poll──▶ 6 HA calendar entities (5 filtered + 1 unfiltered)
Tablet "add event" ────script────▶ MS365 create-event ──▶ Outlook ──▶ Elle's phone
ChoreOps ──▶ per-chore sensors + approve buttons ──▶ Chores view
Mealie ──▶ meal-plan calendar + today's-meal sensors ──▶ Dinner view
Fully Kiosk ──▶ screen / brightness / battery entities ──▶ packages/family_kiosk.yaml
```

### File layout

```
homeassistant/config/
  configuration.yaml           # + frontend resources, + family dashboard registration
  dashboards/
    family.yaml                # view shell only: 5 views, each !include-ing the navbar
    family/
      _navbar.yaml             # bottom tab bar, shared by every view
      home.yaml                # default / idle view
      calendar.yaml
      chores.yaml
      kitchen.yaml
      more.yaml
  packages/
    family_calendar.yaml       # event-composer helpers + create-event scripts
    family_chores.yaml         # ChoreOps glue, Aria's chore→picture map
    family_kitchen.yaml        # timer helpers + preset scripts + Mealie glue
    family_kiosk.yaml          # wake/dim, idle-return, watchdog, PIN gate
  themes/
    family.yaml                # NEW directory — configuration.yaml already !include's it
  www/family/
    chores/*.png               # Aria's chore artwork (tracked; www/community/ is ignored, www/ is not)
    people/*.png               # avatars
```

`themes/` does not currently exist even though `configuration.yaml` includes it with
`!include_dir_merge_named themes`. This design creates it.

### Navigation — five tabs

`navbar-card` renders a fixed bottom tab bar, `!include`d into every view so there is one
definition. `kiosk-mode` strips HA's header and sidebar on the tablet only (matched by user agent /
device), leaving them intact on phones and laptops.

| Tab | Icon | Contents |
|---|---|---|
| **Home** | house | Default view. Big date + weather, today's events, tonight's dinner, Aria's remaining chores, running timers. The "glance" screen — visible ~95% of the time. |
| **Calendar** | calendar | Week planner (default), month toggle, add-event button. |
| **Chores** | star | Aria's picture chores, star balance, rewards. |
| **Kitchen** | pot | Weekly dinner plan, tonight's recipe, timers. |
| **More** | dots | Lights/climate, family map, cameras, garage — the last three PIN-gated. |

Five is deliberate: bottom tab bars degrade badly past six, and every tap target must survive a
4-year-old's aim. Minimum target size 64 px.

### Theme

Light, not dark. A kitchen in daylight makes a dark dashboard unreadable, and Skylight's own visual
identity is bright. Night legibility is handled by dimming the backlight, not by swapping themes —
one theme, one set of behaviours to reason about.

Per-person accent colours, used consistently across calendar, chores and avatars:

| Person | Colour |
|---|---|
| Nick | `#2E86DE` blue |
| Elle | `#A55EEA` purple |
| Aria | `#FF6B9D` pink |
| Eden | `#26C6A0` teal |
| Family / shared | `#F7B731` amber |

Base font scale is raised substantially over the HA default; headline elements (date, tonight's
dinner, timer countdown) are sized to be read from ~3 m.

### Calendar

**Structure.** One shared Outlook calendar named `Family`, owned by Nick, shared read/write with
Elle. Per-person events carry a subject prefix: `Aria: Swimming`. Unprefixed events are whole-family.

**Why a prefix and not Outlook categories.** MS365-Calendar can derive multiple HA calendar entities
from a single `cal_id` by listing several `entities` under it, each with its own `search` (subject
contains, regex-capable) and `exclude` (list of regex patterns) filter. It has no category-based
filtering. The subject prefix is therefore the only available mechanism for per-person entities, and
it is the same approach the DIY thread used.

Six entities from one calendar, configured in `ms365_calendars_*.yaml`:

| Entity | Filter |
|---|---|
| `calendar.family_all` | none |
| `calendar.family_nick` | `search: "Nick:"` |
| `calendar.family_elle` | `search: "Elle:"` |
| `calendar.family_aria` | `search: "Aria:"` |
| `calendar.family_eden` | `search: "Eden:"` |
| `calendar.family_shared` | `exclude: ["Nick:", "Elle:", "Aria:", "Eden:"]` |

**Writing events.** The add-event flow is a pop-up on the Calendar tab: person picker (avatar
buttons) → title → date → time or all-day. A script in `packages/family_calendar.yaml` composes
`"{{ prefix }}{{ title }}"` and calls the MS365 create-event action, so **nobody types the prefix**.
The convention is only load-bearing when someone adds an event directly from Outlook; getting it
wrong is cosmetic (the event appears under "family" instead of a person) and is fixed by renaming.

The exact MS365 action name and parameter set is confirmed against the installed version during the
calendar phase rather than assumed here.

**Display.** `week-planner-card` is the primary candidate — it is what the DIY thread used and it
gives the multi-day column layout that produces the Skylight look. `atomic-calendar-revive` is the
fallback if it doesn't take our per-person colouring cleanly. Colour comes from the per-person
entity, not from parsing the subject at render time.

### Chores

ChoreOps (HACS) is the engine — the successor to KidsChores, which was archived in March 2026.
ChoreOps supplies recurring schedules, points, badges, streaks, rewards and parent approval as
entities; we render them.

Aria's view is a grid of large picture tiles, one per chore, each showing artwork rather than text —
she cannot read. Tapping a tile marks the chore done, which enters ChoreOps' approval queue and
fires a notification to Nick's and Elle's phones. On approval the tile animates to a completed
state and her star balance increases.

Starter chore set, adjustable without code changes once ChoreOps is configured: get dressed, brush
teeth (morning), tidy toys, put shoes away, help set the table, brush teeth (night).

Rewards are configured in ChoreOps as star thresholds. The Chores view shows the star balance as a
progress bar toward the next reward, because a 4-year-old needs to see how close she is, not a
number she can't yet interpret.

Eden has no chores and no chore tiles, but does appear on the calendar (daycare, naps) with her own
colour.

### Kitchen

**Dinner.** Mealie runs as a new service in `docker-compose.yml`, host networking like everything
else, volume `./mealie/` — which must be added to `.gitignore` alongside `scrypted/` and `nodered/`.
HA's native Mealie integration provides a meal-plan calendar plus today's breakfast/lunch/dinner
sensors.

The Kitchen tab shows the week's dinners as seven cards. Tonight's is enlarged. Tapping any dinner
opens the full recipe — ingredients and method — sized to read while cooking. Meal plans support
plain-text entries, so the week can be planned as bare names before any recipe library exists.

**Timers.** Four `timer` helpers in `packages/family_kitchen.yaml`. Presets (eggs 6 min, pasta
10 min, rice 20 min, plus a custom dial) start a timer in one tap. Running timers appear on both the
Kitchen tab and the Home tab. On expiry the tablet plays a sound through Fully Kiosk's media player
and shows a full-width banner until dismissed — a timer that finishes silently on a wall panel is
worthless.

### Kiosk behaviour

`packages/family_kiosk.yaml` makes the tablet an appliance rather than a browser someone left open:

- **Wake on approach.** Fully Kiosk's device-side motion detection wakes the screen and restores
  full brightness. Whether the HA integration also exposes motion as an entity is verified during
  the kiosk phase; the device-side setting is the reliable path and does not depend on HA.
- **Idle dim.** No interaction for 3 minutes → reduce brightness.
- **Night.** 21:30–06:00 → minimum brightness; Fully Kiosk's built-in screensaver shows a photo
  slideshow, giving the Skylight photo-frame behaviour for free.
- **Return home.** Abandoned for 2 minutes on any tab → `fully_kiosk.load_url` back to the Home
  view, so nobody finds it parked on the camera feed the next morning.
- **Self-recovery.** Browser restart daily at 04:00, guarding against renderer memory growth on a
  page that never unloads.
- **Watchdog.** Tablet unreachable for more than 30 minutes → notify Nick's phone, matching the
  host-health alerting pattern already in `packages/extras.yaml`.

### PIN gate

Aria may freely: tick chores, start timers, control lights and music, and view calendar and dinner.
Behind a PIN: garage door, door locks, alarm, and camera feeds.

Implementation: `input_text.family_pin_entry` plus `input_boolean.family_unlocked`, with a large
numeric keypad rendered as a pop-up. The PIN itself is `!secret family_pin`. Unlock persists for
5 minutes, then auto-relocks; it also relocks immediately on leaving the More tab.

This is a child-proof latch, not a security boundary — anyone who can reach the tablet could reach
the garage button by other means. It is sized to stop a 4-year-old, and the spec claims nothing
more. Gated cards are conditionally rendered on `input_boolean.family_unlocked`, so the controls are
absent from the DOM rather than merely visually hidden.

### Failure modes

Every card backed by an external service degrades to a friendly message. A red HA error box on a
kitchen wall is both ugly and meaningless to the people looking at it.

| Failure | Behaviour |
|---|---|
| MS365 token expires / Entra secret rotates | Calendar entities go unavailable. A template sensor detects this and notifies Nick's phone. Calendar view shows "Calendar is reconnecting". |
| Mealie container down | Dinner cards show "No dinner planned yet", not an error. |
| ChoreOps unavailable | Chores view shows a static "Chores are having a nap" panel; no broken tiles. |
| Create-event call fails | Script catches the failure and flashes an error banner on the tablet; the composer keeps its input so nothing is retyped. |
| Tablet offline | Watchdog notification after 30 min. |
| HACS card updated and breaks | Known pre-existing hazard of `mode: yaml` — the repo already documents it. Hard-refresh after card updates. |

### Testing

`check_config` in the deploy pipeline catches YAML and schema errors and rolls back automatically,
so parse-level correctness is already enforced. It does **not** catch references to entities that
don't exist. Therefore each phase verifies, before the next begins:

1. Every entity the phase's cards reference exists and is not `unavailable` (checked in Developer
   Tools → States).
2. The view renders on the actual tablet, not just a laptop browser — layout at the tablet's
   resolution is the thing being tested.
3. Any write path (create event, complete chore, start timer) is exercised end-to-end and confirmed
   at the far end: an event created on the tablet is confirmed visible in Outlook on a phone.

---

## Phases

Ordered so that entities always exist before cards reference them, and so the piece with external
dependencies and the longest lead time comes first.

**Phase 0 — Prerequisites and infrastructure.** Entra ID app registration; create and share the
`Family` Outlook calendar; install MS365-Calendar, ChoreOps, and the frontend cards via HACS;
declare every card in `lovelace.resources`; add the Mealie service to `docker-compose.yml` and
`mealie/` to `.gitignore`. Exit criterion: all six calendar entities, ChoreOps entities and Mealie
sensors exist and are populated. No dashboard work.

**Phase 1 — Tablet as an appliance.** Mount the tablet, install Fully Kiosk and its HA integration,
write `packages/family_kiosk.yaml`. Exit criterion: screen wakes on approach, dims at night, and the
watchdog fires when the tablet is powered off.

**Phase 2 — Shell.** `themes/family.yaml`, `dashboards/family.yaml`, `_navbar.yaml`, and five views
containing placeholders. Exit criterion: tab bar navigates between five themed, empty views on the
tablet with no HA chrome visible.

**Phase 3 — Calendar.** Filtered entities, week planner, add-event composer,
`packages/family_calendar.yaml`. Exit criterion: an event created on the tablet appears in Outlook
on Elle's phone with the correct prefix and colour.

**Phase 4 — Chores.** ChoreOps configuration, artwork in `www/family/chores/`, the tile grid, star
balance, approval notifications. Exit criterion: Aria completes a chore, Nick approves from his
phone, her star balance increases on the tablet.

**Phase 5 — Kitchen.** Mealie meal plan, recipe view, timers. Exit criterion: a week is planned from
a phone and appears on the tablet; a timer completes audibly.

**Phase 6 — More tab and PIN gate.** Lights/climate, family map, camera links, PIN keypad, gated
cards. Exit criterion: gated controls are absent from the DOM until the PIN is entered, and relock
on navigation.

**Phase 7 — Home view.** Built last, because it summarises all of the above. Exit criterion: date,
weather, today's events, tonight's dinner, Aria's remaining chores and running timers all render
live.

## Risks and rollback

| Risk | Mitigation |
|---|---|
| **The O365 tenant forbids app registration, or requires admin consent.** This is the single biggest threat — it blocks Phases 0 and 3 entirely and is outside our control. | Establish this on day one, before anything else is built. If the work tenant blocks it, fall back to a free personal Outlook.com account used solely as the family calendar and shared with both adults. Elle's phone sees it identically. |
| Phases 4–7 all depend on Phase 0 external services | Phases 1 and 2 depend on none of them and can proceed in parallel while the Entra registration is sorted. |
| ChoreOps' hand-built view proves disproportionate | Named fallback: register ChoreOps' shipped dashboard as a second storage-mode dashboard that the Chores tab links to. Accepts one non-version-controlled dashboard and a visible styling seam; taken only if Phase 4 stalls. |
| Mealie load on the mac mini | Deploy it first, in Phase 0, and observe before anything depends on it. It is a small service, but the host has known socket-pressure sensitivity documented in `packages/extras.yaml`. |
| AMOLED burn-in from a static dashboard | Night dimming, screensaver slideshow, and the return-home behaviour together mean no single frame is displayed continuously. |
| A bad dashboard push takes HA down | Already mitigated: `check_config` runs before restart and hard-resets on failure. |
| A card update breaks rendering in `mode: yaml` | Pre-existing, documented in `configuration.yaml`. Not made worse by this work. |

## Prerequisites on the user

1. **Confirm whether the O365 tenant permits an Entra ID app registration** — do this first.
2. Create the shared `Family` calendar in Outlook and share it read/write with Elle.
3. Buy and mount the Galaxy Tab, with in-wall USB-C power.
4. Buy the Fully Kiosk Plus licence (~$8, one-time).
5. Choose the PIN; it goes in `secrets.yaml` as `family_pin`.
6. Supply or approve chore artwork for Aria's six starter chores.
7. Confirm where Mealie's volume should live on the mac mini.
