# tra-commute-bot

Every morning, on whatever schedule you set up over Telegram, a message tells
you which train to catch.

Not the timetable — the timetable is on your phone already. This works out,
from live TRA delay data, which train you can *actually* board given when you
can be at the station, and which of those gets you to your destination station
earliest. If none of them gets you there on time, it tells you how much
earlier you would have to leave to fix that, and which train number to quote
at the counter when you ask for a delay certificate.

```
通勤簡報 · 8/18 (二) 07:50

建議搭乘 2008 區間快
桃園 08:26 → 臺北 09:02
準點 · 預計 09:02 抵達（餘裕 8 分）

其他選項
NO.   DLY    DEP    ARR
2008   +0  08:26  09:02  REC
1138   +0  08:34  09:14  LATE
1136   +0  08:16  08:57  GONE

DLY 誤點　DEP 發車　ARR 抵北
REC 建議　LATE 遲到　GONE 已過

以 08:20 抵站計算
資料更新 07:49:53
```

You set up a rule ("Schedule") once, over Telegram — the days and time to be
notified, the route, the earliest you can be at the origin station, the
latest you can accept arriving at the destination station — and can run
several of these side by side (a morning commute and an evening one, say).
The full specification, including the reasoning behind each design decision,
is in [`spec.md`](spec.md).

## Why it is not just a timetable lookup

The interesting case is the one where intuition is wrong. On this route the
express 2008 (08:26 → 09:02) is normally the right answer. But when everything
runs ten minutes late, the *local* 1136 becomes catchable — and because it left
ten minutes earlier, it still reaches Taipei five minutes before 2008 does.
Those five minutes are the difference between arriving on time and running late.

So the program never assumes the express is faster. It compares estimated
arrivals, every morning, from live data.

## Setting up a Schedule

Message the bot:

- `/setup` — build a new Schedule: a name, origin and destination station,
  earliest ready time, latest acceptable arrival, notify days and time, and
  how many minutes early you're willing to leave if today's trains are running
  late. Every question ends with buttons where one makes sense — you never
  need to remember a command syntax mid-flow.
- `/manage` — list your Schedules, open one, and change any single field (or
  delete the whole rule) without redoing the rest.
- `/status` — each Schedule's route and today's delivery result.
- `/help` — the command list, or a nudge toward `/setup` if you have none yet.
- `/cancel` — abandon whatever's in progress; nothing is saved until a flow's
  final confirmation.

A Schedule is either fully set up or it does not exist — there is no
in-between state to get stuck in.

## Design

Dependencies point inwards. `internal/domain` holds the model and the
algorithms and imports nothing outside the standard library; `internal/usecase`
orchestrates a run and declares the interfaces it needs; the adapters implement
those interfaces; `internal/command` is the Telegram command/callback state
machine built on top of them; `cmd/tracommute` wires everything together.

```
cmd/tracommute/          composition root: flags, wiring, the two loops
cmd/gen-stations/        one-time codegen: TDX /Station -> stations_data.go
internal/
  domain/                pure model and algorithms, no I/O
    train.go             services, candidates, delay sources
    params.go             the user's constraints as absolute instants
    classify.go          catchability axis (§7.4)
    traintype.go         electronic-ticket eligibility (A13)
    plan.go              window, filter, lexicographic ranking (§7.5)
    compensate.go        the "leave earlier" search (§7.8)
    certificate.go       which train to quote at the counter (§7.6)
    brief.go             which message template applies
    schedule.go          the tick guard, evaluated per-Schedule (§10.8)
    settings.go          Schedule / SettingsList model (§10.1)
    station.go           MatchStations, for /setup's station question
    stations_data.go     generated: the full TRA station catalog
  usecase/
    ports.go             interfaces the orchestration depends on
    brief.go             fetch → plan → render → deliver
    tick.go              guard every Schedule, run the due ones, record
    settingsactor.go      single-goroutine serialized access to settings.json (§10.9)
  adapter/
    tdx/                 TDX v3 client: auth, throttle, 429 backoff, parsing
    telegram/            Bot API: sendMessage, getUpdates, inline keyboards
    render/              message layout and the ASCII-only comparison table
    settingsfile/        atomic JSON settings (a list of Schedules)
    statefile/           atomic JSON tick-guard state
    archive/             raw API responses, pruned after 30 days
  command/                /setup, /manage, /status, /help, /cancel (§10)
  platform/clock/        real and fixed clocks
```

Three properties are load-bearing:

**Nothing fails silently.** Any failure still sends a message. A TDX outage
sends a warning; a live-board failure still sends the scheduled times, clearly
marked as not delay-adjusted. A brief that is computed but not delivered counts
as a failure. A missing notification is indistinguishable from a quiet
morning, and that ambiguity is the thing worth engineering away.

**Schedules live in settings.json, not in the config file or in code.** Each
one is its own weekdays, notify time, route, ready time and deadline, set live
over `/setup` and `/manage` and read fresh on every tick — no restart. Only
the calibration knobs shared by every Schedule (risk margin, the train-type
filter, delay-certificate and severe-delay thresholds, TDX throttling) live in
`config.yaml`, because those change by admin decision, not by commute.

**One long-running process, two independent loops.** A notify loop wakes every
minute and decides, per Schedule, whether this is its moment; a command loop
long-polls Telegram for `/setup`, `/manage` and the rest, the whole time the
process is up. Both touch `settings.json`, so a single actor goroutine
serializes every read and write to it (`internal/usecase/settingsactor.go`) —
the rest of the program never talks to the settings store directly. GitHub
Actions' own `schedule:` trigger is not used to drive any of this: its 5-20
minute jitter is incompatible with a system whose whole value is minute-scale
timing.

## Running it locally

Credentials come from the environment. For development, a `.env` file is read
if present (real environment variables always win):

```bash
TDX_CLIENT_ID=...
TDX_CLIENT_SECRET=...
TELEGRAM_BOT_TOKEN=...     # TG_BOT_TOKEN also accepted
TELEGRAM_CHAT_ID=...       # TG_CHAT_ID also accepted
```

Print a brief without sending anything (needs at least one Schedule already in
`.local/settings.json` — set one up against the real bot first, or hand-write
one following `internal/adapter/settingsfile`'s `schedules: [...]` shape):

```bash
go run ./cmd/tracommute -config configs/config.local.yaml -dry-run -force
```

Simulate a specific morning — this is how you debug scheduling logic without
waiting for the clock:

```bash
go run ./cmd/tracommute -config configs/config.local.yaml \
  -dry-run -force -at 2026-08-19T07:50
```

Run the real long-running process locally — both loops, live Telegram
interaction included:

```bash
go run ./cmd/tracommute -config configs/config.local.yaml
```

Under `-dry-run` the message goes to stdout and the logs to stderr, so the
output can be redirected or diffed on its own; `-dry-run` and `-force` never
start the command loop, since they exist to check a message's content, not to
exercise the live-settings interface.

### Flags

| Flag | Meaning |
|---|---|
| `-config` | config file path (default `/etc/tra-commute/config.yaml`) |
| `-at` | simulate a time for a single tick, `2006-01-02T15:04` |
| `-dry-run` | print the message instead of sending; writes no state |
| `-force` | skip the schedule guard, run the first configured Schedule once |
| `-env-file` | development credentials file (default `.env`; empty disables) |
| `-verbose` | debug logging, including full API responses |
| `-version` | print the version and exit |

With none of `-at`, `-dry-run` or `-force`, the program starts the real
long-running process and runs until `SIGTERM`/`SIGINT`.

### Getting a Telegram chat ID

Message your bot once, then:

```bash
curl -s "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/getUpdates" \
  | python3 -c 'import sys,json; [print(u["message"]["chat"]["id"]) for u in json.load(sys.stdin)["result"] if "message" in u]'
```

A private chat ID is positive. A leading `-` means a group.

### Regenerating the station catalog

`/setup`'s station question matches against a catalog embedded at build time
(`internal/domain/stations_data.go`) rather than calling TDX's `/Station` at
runtime (§6.2) — that endpoint is a build-time-only lookup. Regenerate it if
TRA adds or renames a station:

```bash
go run ./cmd/gen-stations
```

## Deploying

Docker is the only supported deployment: a single long-running container,
`docker run --restart unless-stopped` or `docker compose up -d`. There is no
cron and no systemd timer inside the image — `tracommute` itself owns the
schedule via its own two loops (see Design, above).

```bash
cp configs/config.example.yaml configs/config.yaml   # tune it to your commute
cp .example.env env                                   # fill in credentials
docker compose up -d
docker compose logs -f
```

State, `settings.json` and the archive persist in the `tra-commute-data`
named volume, so recreating the container never loses a Schedule or the
day's tick-guard state. Tagged pushes (`vX.Y.Z`) build and publish
`ghcr.io/<owner>/tra-commute-bot` via CI; pull that instead of building
locally if you'd rather not.

## Tests

```bash
go test ./...
go test ./internal/domain/ -cover     # the algorithms; gated at 90% in CI
```

The domain tests are table-driven and cover the five scenarios from the
specification, including the one where the local train beats the express, plus
the multi-Schedule guard (two rules due in the same minute must both fire).
The TDX adapter is tested against `httptest` with fixtures captured from the
real API (see [`testdata/README.md`](testdata/README.md)), covering rate-limit
backoff, both published clock formats, the midnight rollover and cancellation
flags. The renderer's tests assert that the `<pre>` tables stay ASCII-only and
inside the width budget, which is the whole reason for the format. The command
package drives full `/setup` and `/manage` flows — including the weekday
picker and station disambiguation — against a mock Bot API and an in-memory
settings actor.

A pre-commit hook mirrors CI's lint job (`gofmt`, `go vet`,
[`golangci-lint`](https://golangci-lint.run/)) so a failure shows up before
you push instead of after. Enable it once per clone:

```bash
git config core.hooksPath .githooks
```

## Message format

Two constraints shape the output, both discovered on a real handset rather
than in theory.

**The table is ASCII, including its column headings.** Telegram's monospace
font does not render a Chinese glyph at exactly twice the width of a Latin
one, so a grid that is perfectly rectangular by any cell-counting model still
comes out crooked on the phone. Headings are abbreviated (`NO.`, `DLY`, `DEP`,
`ARR`, `LATE`) and translated in a key underneath, where nothing is being
aligned.

**There are no emoji and no box-drawing rules.** Emphasis comes from bold
headings; each row ends with a status word explained by a key. A ruled table
with emoji status labels measured forty characters and wrapped on an iPhone in
portrait, which destroys the alignment that the monospace block existed to
provide. The current layout is twenty-four.

**No line inside the table begins with a space.** Telegram strips leading
whitespace even inside a `<pre>` block. When the status marker led the row and
its "nothing to report" value was blank, those rows silently lost their indent
and slid left while every other row stayed put. Moving the status to the end
of the row makes the train number the first column, so the case cannot arise.

## Calibration

`T_ready` (earliest at the origin station) and the deadline (latest acceptable
arrival at the destination station) are human estimates, set per Schedule via
`/setup` or `/manage`, and they determine whether the advice is any good.
Both are printed at the bottom of every message so drift is noticeable — watch
the real mornings for a week and adjust with `/manage`.

The system's job ends at the destination station platform (§1.3 of the spec):
it does not model or ask about the walk from there to wherever you're actually
going. If you need to account for that, fold it into the deadline you set.

## Scope

No booking, no seat reservation, no web UI, no multi-user support — one
Telegram chat, any number of Schedules. It does not contact anyone on your
behalf and does not draft messages to your manager: it tells you the
situation and the options, and then stops.
