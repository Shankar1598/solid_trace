# M1-03: Full Log normalisation

**What to build:** every Log a JSON sender sends is normalised before the write, so Pebble holds normalised Logs and the latest-N query shows them as the spec describes. The JSON route's response gains the truncated count. Spec: [Log ingest](../spec.md#log-ingest-eventstore) (Normalisation, Response); user stories 10–11 and 15–27.

**Blocked by:** M1-01.

**Status:** ready-for-agent

- [ ] JSON aliases, first present wins and is consumed, lower-ranked ones stay Attributes: `message`, `time`, `level`, `service`, `environment`, `trace_id`, `span_id` as listed in the spec.
- [ ] Time: RFC 3339 / ISO 8601 or a numeric epoch in s, ms, µs or ns (unit from magnitude). A missing timestamp, one older than the Project's Retention (the instance default of 30 days until M1-09 reads per-Project Retention), or one more than 1 hour in the future becomes the receipt time, the given value kept as `original.time`. Use the controllable clock.
- [ ] Levels: the six Levels case-insensitively, with `warning`, `err`, `critical`/`crit`/`panic`/`emergency`, `notice` mapped; missing or unrecognised leaves no Level, an unrecognised one kept as `original.level`. A malformed trace id is kept as `original.trace_id`.
- [ ] `message` is always a string, possibly empty.
- [ ] Flattening to dotted names; `{"a.b": …}` beats `{"a": {"b": …}}` ([ADR 0002](../../../docs/adr/0002-logs-scoped-by-project-flat-attributes.md)); arrays are one value and objects inside arrays aren't flattened.
- [ ] Types: each value keeps its type; a whole number a DOUBLE can't hold exactly is stored as a string.
- [ ] Limits: strings truncated at 64 KB; Attribute names over 256 B dropped; past 1,000 Attributes the excess dropped in a stable order; nesting past 10 levels kept as a JSON string from that depth; a Log still over 1 MB dropped and counted. A truncated Log carries `solidtrace.truncated = true`.
- [ ] A sender's `solidtrace.*` key becomes `original.solidtrace.*`.
- [ ] The JSON route's `200` reports Logs accepted, dropped and truncated.
- [ ] Tests act as a sender: POST, process, read back through the latest-N query; one case per alias, Level spelling, time form and rule above.
