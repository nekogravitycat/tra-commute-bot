# CLAUDE.md

Development notes for this repo. See [`README.md`](README.md) for what the
bot does and how to run it; [`spec.md`](spec.md) for the full specification
and the reasoning behind each design decision.

## Why it is not just a timetable lookup

The interesting case is the one where intuition is wrong. On a route where
the express is normally the right answer, if everything runs ten minutes
late, a local train that left earlier can still become catchable — and
because it left earlier, it can still reach the destination before the
express does. Those minutes are the difference between arriving on time and
running late.

So the program never assumes the express is faster. It compares estimated
arrivals, every morning, from live data. Keep this in mind when touching
`internal/domain/plan.go` or `classify.go`: ranking must stay based on
computed arrival time, never on train type or scheduled departure order.

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
    actor.go             single-goroutine serialized access to a JSON document
                         — settings.json and state.json each get one (§10.9)
  adapter/
    tdx/                 TDX v3 client: auth, throttle, 429 backoff, parsing
    telegram/            Bot API: sendMessage, getUpdates, inline keyboards
    render/              message layout and the ASCII-only comparison table
    settingsfile/        atomic JSON settings (a list of Schedules)
    statefile/           atomic JSON tick-guard state
    archive/             raw API responses, pruned after 30 days
  command/                /setup, /manage, /status, /help, /cancel (§10)
  platform/clock/        real and fixed clocks
  platform/atomicfile/   write-temp-then-rename, shared by every file we own
```

Three properties are load-bearing — preserve them in any change:

**Nothing fails silently.** Any failure still sends a message. A TDX outage
sends a warning; a live-board failure still sends the scheduled times, clearly
marked as not delay-adjusted. A brief that is computed but not delivered counts
as a failure. A missing notification is indistinguishable from a quiet
morning, and that ambiguity is the thing worth engineering away. If you add a
new failure path, make sure it still results in a Telegram message.

**Schedules live in settings.json, not in the config file or in code.** Each
one is its own weekdays, notify time, route, ready time and deadline, set live
over `/setup` and `/manage` and read fresh on every tick — no restart. Only
the calibration knobs shared by every Schedule (risk margin, the train-type
filter, delay-certificate and severe-delay thresholds, TDX throttling) live in
`config.yaml`, because those change by admin decision, not by commute. Don't
move per-Schedule state into `config.yaml`, and don't move shared calibration
knobs into `settings.json`.

**One long-running process, two independent loops.** A notify loop wakes every
minute and decides, per Schedule, whether this is its moment; a command loop
long-polls Telegram for `/setup`, `/manage` and the rest, the whole time the
process is up. Both touch `settings.json` and `state.json`, so each file gets
a single actor goroutine that serializes every read and write to it
(`internal/usecase/actor.go`) — the rest of the program never talks to either
store directly. GitHub Actions' own `schedule:` trigger is not used to drive
any of this: its 5-20 minute jitter is incompatible with a system whose whole
value is minute-scale timing. Never add code that bypasses an actor to read or
write `settings.json` or `state.json` directly.

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

Keep `internal/domain` coverage at or above 90% — CI gates on it. New
scenarios in the domain package should follow the existing table-driven style.

A pre-commit hook mirrors CI's lint job (`gofmt`, `go vet`,
[`golangci-lint`](https://golangci-lint.run/)) so a failure shows up before
you push instead of after. Enable it once per clone:

```bash
git config core.hooksPath .githooks
```

## Message format

Two constraints shape the output, both discovered on a real handset rather
than in theory. Respect them in any change to `internal/adapter/render`.

**The table is ASCII, including its column headings.** Telegram's monospace
font does not render a Chinese glyph at exactly twice the width of a Latin
one, so a grid that is perfectly rectangular by any cell-counting model still
comes out crooked on the phone. Headings are abbreviated (`NO.`, `DLY`, `DEP`,
`ARR`) and the status column carries no heading at all — its labels (`REC`,
`RISK`, `LATE`, `GONE`, `OK`) read as themselves. There is deliberately no key
translating them underneath (`TestNoLegendUnderTable` guards this); don't
reintroduce one.

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
Both are printed at the bottom of every message so drift is noticeable.

The system's job ends at the destination station platform (§1.3 of the spec):
it does not model or ask about the walk from there to wherever the user is
actually going. Don't add that modeling — it belongs in the deadline the user
sets, not in the domain logic.

## Scope

No booking, no seat reservation, no web UI, no multi-user support — one
Telegram chat, any number of Schedules. It does not contact anyone on the
user's behalf and does not draft messages to their manager: it tells them the
situation and the options, and then stops. Keep new features within this
scope; if a request implies booking, multi-user accounts, or a web UI, flag
that it's outside the project's stated scope before implementing it.
