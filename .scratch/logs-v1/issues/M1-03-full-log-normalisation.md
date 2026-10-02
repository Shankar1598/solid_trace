# M1-03: Full Log normalisation

**What to build:** every Log a JSON sender sends is normalised before the write, so Pebble holds normalised Logs and the latest-N query shows them as the spec describes. The JSON route's response gains the truncated count. Spec: [Log ingest](../spec.md#log-ingest-eventstore) (Normalisation, Response); user stories 10–11 and 15–27.

**Blocked by:** M1-01.

**Status:** ready-for-agent

- [x] JSON aliases, first present wins and is consumed, lower-ranked ones stay Attributes: `message`, `time`, `level`, `service`, `environment`, `trace_id`, `span_id` as listed in the spec.
- [x] Time: RFC 3339 / ISO 8601 or a numeric epoch in s, ms, µs or ns (unit from magnitude). A missing timestamp, one older than the Project's Retention (the instance default of 30 days until M1-09 reads per-Project Retention), or one more than 1 hour in the future becomes the receipt time, the given value kept as `original.time`. Use the controllable clock.
- [x] Levels: the six Levels case-insensitively, with `warning`, `err`, `critical`/`crit`/`panic`/`emergency`, `notice` mapped; missing or unrecognised leaves no Level, an unrecognised one kept as `original.level`. A malformed trace id is kept as `original.trace_id`.
- [x] `message` is always a string, possibly empty.
- [x] Flattening to dotted names; `{"a.b": …}` beats `{"a": {"b": …}}` ([ADR 0002](../../../docs/adr/0002-logs-scoped-by-project-flat-attributes.md)); arrays are one value and objects inside arrays aren't flattened.
- [x] Types: each value keeps its type; a whole number a DOUBLE can't hold exactly is stored as a string.
- [x] Limits: strings truncated at 64 KB; Attribute names over 256 B dropped; past 1,000 Attributes the excess dropped in a stable order; nesting past 10 levels kept as a JSON string from that depth; a Log still over 1 MB dropped and counted. A truncated Log carries `solidtrace.truncated = true`.
- [x] A sender's `solidtrace.*` key becomes `original.solidtrace.*`.
- [x] The JSON route's `200` reports Logs accepted, dropped and truncated.
- [x] Tests act as a sender: POST, process, read back through the latest-N query; one case per alias, Level spelling, time form and rule above.

## Comments

### 2026-10-02 built (logs-v1/m1-03)

What was built:
- **The normaliser** is its own package, `ingest/normalise`, pure and tested directly (`normalise_test.go`). Interface: `normalise.JSON(object map[string]json.RawMessage, normalise.Receipt{At, RetentionDays}) (models.Log, normalise.Outcome)`. `Outcome` is `Kept`, `Truncated` (stored, carries `solidtrace.truncated = true`) or `Dropped` (over 1 MB, not stored). It replaces M1-01's `ingest/normalise.go`.
- Inside, a source's front end picks the value sent for each built-in field into a `sent` (the seven raw values plus the remaining Attributes under flat names), and `normaliseSent` applies every shared rule: field parsing, `original.<field>`, the reserved prefix, types, limits, the time window. `JSON` is the JSON front end: flatten, then the aliases.
- **Retention seam:** `ingest.Retention` (`LogRetentionDays(projectID uint32) (int, error)`) on `ingest.Config.Retention`. `storage.ConsoleSettings` already satisfies it. `ingest.FixedRetention(30)` is wired in `main.go` and the test env until M1-09. `IngestLogs` reads it once per request; a read error fails the request (500) and stores nothing.
- **Response:** `ingest.LogCounts` gained `Truncated`; the route answers `{"accepted": n, "dropped": m, "truncated": k}`. Truncated Logs are among the accepted ones. A Log over 1 MB counts as dropped.
- **Tests:** `logs_normalise_test.go` (Seam 1, five environments, about 20 s): POST one array of cases, process, read back through the latest-N query, one case per alias, Level spelling, time form and rule. `ingest/service_test.go` checks that each Project's Retention decides which times are too old, through a fake `Retention`.

Choices the ticket left open:
- **Aliases are looked up after flattening**, so `{"log": {"level": …}}`, `{"service": {"name": …}}` and `{"deployment": {"environment": …}}` fill their fields like the dotted keys. "Present" means not null. The first alias present wins even when its value is unusable: the value becomes `original.<field>` and lower-ranked aliases stay Attributes (ticket 08's literal reading).
- **`message`, `service`, `environment`** take a JSON string only. Any other value is kept as `original.<field>` and is not stringified; `message` is then `""`.
- **Trace and span ids:** 32 and 16 hex digits, any case, stored lower-case. Anything else is `original.trace_id` / `original.span_id` (the spec names only the trace id; span id follows it).
- **Levels:** exact spellings, case-insensitive, no other trimming. Numeric levels (pino's `30`, OTLP severity numbers) are unrecognised and kept as `original.level`.
- **Time forms:** RFC 3339 with `T` or a space, lower-case `t`/`z`, offsets `±hh:mm`, `±hhmm`, `±hh` or none (UTC), fractional seconds. Date-only strings and epochs written as strings are not times (`original.time`). A numeric epoch's unit is s below 1e11, ms below 1e14, µs below 1e17, else ns. A plain decimal is read exactly to the nanosecond; one with an exponent is read as a DOUBLE. DuckDB keeps microseconds.
- **Time window:** a time before `At` minus Retention days, or after `At` plus 1 hour, becomes the receipt time. Both boundaries are inclusive (kept).
- **Dotted-key clash:** the form nested fewer levels wins. Between equals (`{"a": {"b.c"}}` vs `{"a.b": {"c"}}`), the longer outer key wins, so the result never depends on key order.
- **Depth:** top-level keys are level 1. An object at level 10 is stored whole as JSON text in `attrs_json`, and is listed as an object. That isn't counted as truncation: nothing is lost.
- **What marks a Log truncated:** a string cut, a name dropped, Attributes past 1,000 dropped.
- **64 KB** applies to every stored string: built-in fields, `original.*`, and numbers kept as strings. Strings inside arrays and deep JSON aren't cut; they are bounded only by the 1 MB rule.
- **256 B** applies to the final name: the flattened path, and the `original.` prefix where one is added.
- **1,000 Attributes** counts every Attribute, `original.*` included, and keeps the first 1,000 in byte order of name. `solidtrace.truncated` is added on top.
- **1 MB** is measured after the cuts: the built-in fields plus each Attribute's name and value (a number counts 8 B, a boolean 1 B).
- **Overwrites:** a sender's own `original.<field>` is overwritten by SolidTrace's when that field's value is unusable. Nulls and empty objects store nothing.
- **Hardening:** an epoch like `1e-999999` and whole numbers of more than 309 digits are read in bounded time. `big.Rat`/`big.Int` would have taken seconds or worse on such input.

Deviations: none from the spec. The one reading worth challenging is "strings truncated at 64 KB" not reaching strings inside arrays.

Follow-ups for later tickets:
- **M1-05:** add a Sentry front end in `ingest/normalise` (for example `normalise.Sentry(record, receipt)`). It should build a `sent` directly: `body`, `timestamp`, `level`, `trace_id`, `sentry.environment` and `service.name` into `fields`; the unwrapped attributes, through `flatten`, into `attributes`. Then call `normaliseSent`. Don't route Sentry records through `JSON`: its aliases would consume Sentry attributes such as `msg`, `env` or `ts`. Float-second `timestamp`s already parse exactly.
- **M1-09:** pass `storage.ConsoleSettings` as `ingest.Config.Retention` in `main.go` (and the test env) instead of `ingest.FixedRetention(30)`, perhaps behind a small cache since it is read on every Log request.
- Possibly map pino/bunyan numeric levels (10–60); not in the spec.

