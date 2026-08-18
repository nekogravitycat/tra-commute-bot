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
1138   +0  08:34  09:14  LATE 4m
1136   +0  08:16  08:57  GONE

以 08:20 抵站計算
資料更新 07:49:53
```

You set up a rule ("Schedule") once, over Telegram — the days and time to be
notified, the route, the earliest you can be at the origin station, the
latest you can accept arriving at the destination station — and can run
several of these side by side (a morning commute and an evening one, say).
The full specification, including the reasoning behind each design decision,
is in [`spec.md`](spec.md).

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
rather than calling TDX's `/Station` at runtime — that endpoint is a
build-time-only lookup. Regenerate it if TRA adds or renames a station:

```bash
go run ./cmd/gen-stations
```

## Deploying

Docker is the only supported deployment: a single long-running container,
`docker run --restart unless-stopped` or `docker compose up -d`. There is no
cron and no systemd timer inside the image — `tracommute` itself owns the
schedule via its own two loops.

```bash
cp configs/config.example.yaml configs/config.yaml   # tune it to your commute
cp .example.env .env                                   # fill in credentials
docker compose up -d
docker compose logs -f
```

State, `settings.json` and the archive persist in the `tra-commute-data`
named volume, so recreating the container never loses a Schedule or the
day's tick-guard state. Tagged pushes (`vX.Y.Z`) build and publish
`ghcr.io/<owner>/tra-commute-bot` via CI; pull that instead of building
locally if you'd rather not.
