# Test fixtures

| File | Origin |
|---|---|
| `od_timetable.json` | **Real capture**, 2026-08-18, `DailyTrainTimetable/OD/1080/to/1000/2026-08-18`. Trimmed to trains 1136 / 1138 / 2008 / 272 / 278; every field is otherwise untouched. |
| `live_board.json` | **Hand-built from the real response shape.** The actual `TrainLiveBoard` capture contained none of the commute-hour trains, because they were not running at capture time, so the real field names and formats were kept and this project's train numbers filled in. Includes one negative `DelayTime` to exercise the clamp. |

Both are v3 responses: an object wrapping the array, clock times as `HH:mm`
(no seconds, no zone), and `UpdateTime` as RFC3339 with a zone.

The timetable capture is also the evidence behind two decisions recorded in
`spec.md`: that the OD endpoint returns only the two endpoint stops rather than
the whole run, and that `TrainTypeCode` cannot distinguish 普悠瑪 from an
ordinary 自強.
