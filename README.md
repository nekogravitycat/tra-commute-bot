# tra-commute-bot

Every weekday at 07:50, a Telegram message tells you which train to catch.

Not the timetable — the timetable is on your phone already. This works out,
from live TRA delay data, which train you can *actually* board given when you
can be at the station, and which of those gets you to work earliest. If none of
them gets you there on time, it tells you how much earlier you would have to
leave to fix that, and which train number to quote at the counter when you ask
for a delay certificate.

```
通勤簡報 · 8/18 (二) 07:50

建議搭乘 2008 區間快
桃園 08:26 → 臺北 09:02
準點 · 預計 09:22 打卡（餘裕 8 分）

其他選項
NO.   DLY    DEP    ARR
2008   +0  08:26  09:02  REC
1138   +0  08:34  09:14  LATE
1136   +0  08:16  08:57  GONE

DLY 誤點　DEP 發車　ARR 抵北
REC 建議　LATE 遲到　GONE 已過

以 08:20 抵站、末端 20 分計算
資料更新 07:49:53
```

The full specification, including the reasoning behind each design decision, is
in [`spec.md`](spec.md).

## Why it is not just a timetable lookup

The interesting case is the one where intuition is wrong. On this route the
express 2008 (08:26 → 09:02) is normally the right answer. But when everything
runs ten minutes late, the *local* 1136 becomes catchable — and because it left
ten minutes earlier, it still reaches Taipei five minutes before 2008 does.
Those five minutes are the difference between clocking in at 09:27 and at 09:32.

So the program never assumes the express is faster. It compares estimated
arrivals, every morning, from live data.

## Design

Dependencies point inwards. `internal/domain` holds the model and the
algorithms and imports nothing outside the standard library; `internal/usecase`
orchestrates a run and declares the interfaces it needs; the adapters implement
those interfaces; `cmd/tracommute` wires them together.

```
cmd/tracommute/          composition root: flags, wiring, exit codes
internal/
  domain/                pure model and algorithms, no I/O
    train.go             services, candidates, delay sources
    params.go            the user's constraints as absolute instants
    classify.go          catchability axis (§7.4)
    traintype.go         electronic-ticket eligibility (A13)
    plan.go              window, filter, lexicographic ranking (§7.5)
    compensate.go        the "leave earlier" search (§7.8)
    certificate.go       which train to quote at the counter (§7.6)
    brief.go             which message template applies
    schedule.go          the tick guard (§10.3)
  usecase/
    ports.go             interfaces the orchestration depends on
    brief.go             fetch → plan → render → deliver
    tick.go              guard, run, record
  adapter/
    tdx/                 TDX v3 client: auth, throttle, 429 backoff, parsing
    telegram/            Bot API delivery
    render/              message layout and the ASCII-only comparison table
    statefile/           atomic JSON state
    archive/             raw API responses, pruned after 30 days
  platform/clock/        real and fixed clocks
```

Two properties are load-bearing:

**Nothing fails silently.** Any failure still sends a message. A TDX outage
sends a warning; a live-board failure still sends the scheduled times, clearly
marked as not delay-adjusted. A brief that is computed but not delivered counts
as a failure and exits non-zero. A missing notification is indistinguishable
from a quiet morning, and that ambiguity is the thing worth engineering away.

**The schedule lives in the config, not in systemd.** The timer fires every
minute and the program decides whether this minute is the scheduled one. So
changing when the brief arrives is a config edit that takes effect next minute:
no sudo, no `daemon-reload`. A run that fails at 07:50 retries on the following
ticks for ten minutes, then gives up once, loudly. Almost all of the 1440 daily
wake-ups exit within milliseconds without touching the network.

## Running it locally

Credentials come from the environment. For development, a `.env` file is read
if present (real environment variables always win):

```bash
TDX_CLIENT_ID=...
TDX_CLIENT_SECRET=...
TELEGRAM_BOT_TOKEN=...     # TG_BOT_TOKEN also accepted
TELEGRAM_CHAT_ID=...       # TG_CHAT_ID also accepted
```

Print the brief without sending anything:

```bash
go run ./cmd/tracommute -config configs/config.local.yaml -dry-run -force
```

Simulate a specific morning — this is how you debug a program that otherwise
only runs once a day:

```bash
go run ./cmd/tracommute -config configs/config.local.yaml \
  -dry-run -force -at 2026-08-19T07:50
```

Under `-dry-run` the message goes to stdout and the logs to stderr, so the
output can be redirected or diffed on its own.

### Flags

| Flag | Meaning |
|---|---|
| `-config` | config file path (default `/etc/tra-commute/config.yaml`) |
| `-at` | simulate a time, `2006-01-02T15:04` |
| `-dry-run` | print the message instead of sending; writes no state |
| `-force` | skip the schedule guard and run once |
| `-env-file` | development credentials file (default `.env`; empty disables) |
| `-verbose` | debug logging, including full API responses |
| `-version` | print the version and exit |

### Getting a Telegram chat ID

Message your bot once, then:

```bash
curl -s "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/getUpdates" \
  | python3 -c 'import sys,json; [print(u["message"]["chat"]["id"]) for u in json.load(sys.stdin)["result"] if "message" in u]'
```

A private chat ID is positive. A leading `-` means a group.

## Deploying

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o tracommute ./cmd/tracommute
scp tracommute deploy/ user@host:/tmp/
ssh user@host 'sudo bash /tmp/deploy/install.sh'
```

The installer creates the service account, config directory, credentials
template and systemd units, then prints what is left to do. The binary is
static and embeds the timezone database, so it needs nothing on the host — not
even tzdata, whose absence would otherwise silently shift every time in the
message to UTC.

Verify before enabling the timer:

```bash
sudo -u tracommute tracommute -config /etc/tra-commute/config.yaml \
     -env-file /etc/tra-commute/env -dry-run -force
sudo systemctl enable --now tra-commute.timer
journalctl -u tra-commute -f
```

## Tests

```bash
go test ./...
go test ./internal/domain/ -cover     # the algorithms; gated at 90% in CI
```

The domain tests are table-driven and cover the five scenarios from the
specification, including the one where the local train beats the express. The
TDX adapter is tested against `httptest` with fixtures captured from the real
API (see [`testdata/README.md`](testdata/README.md)), covering rate-limit
backoff, both published clock formats, the midnight rollover and cancellation
flags. The renderer's tests assert that the `<pre>` tables stay ASCII-only and
inside the width budget, which is the whole reason for the format.

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
provide. The current layout is twenty-eight.

**No line inside the table begins with a space.** Telegram strips leading
whitespace even inside a `<pre>` block. When the status marker led the row and
its "nothing to report" value was blank, those rows silently lost their indent
and slid left while every other row stayed put. Moving the status to the end
of the row makes the train number the first column, so the case cannot arise.

## Calibration

Two numbers are human estimates rather than measurements, and they determine
whether the advice is any good:

- `ready_lead_minutes` (30) — how long from the notification to standing at the
  origin station
- `last_mile_minutes` (20) — destination station to clocking in

Both are printed at the bottom of every message so drift is noticeable. Record
the real values for a week and adjust.

## Scope

Single route, one direction, one person. No booking, no seat reservation, no
web UI. It does not contact anyone on your behalf and does not draft messages
to your manager: it tells you the situation and the options, and then stops.
