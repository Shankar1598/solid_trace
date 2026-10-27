# 02: A Transaction appears on the trace page

Spec: [../spec.md](../spec.md)

**What to build:** the tracer bullet. A Transaction sent by a Sentry SDK shows as a trace in Console.

- EventStore's envelope ingest keeps `transaction` items with their sample rate. Span processing stores them as Spans, one segment row plus one row per Span, with self time, group, repeats and the Unparameterised name, in `spans` and `trace_index`, without storing a range twice.
- Spans form a second day-table family whose Retention is trace Retention. Console gains the default (14 days) on instance settings and the per-Project override, and EventStore reads both (ADR 0001).
- A Query API trace route answers a trace across the given Projects.
- The Organization's trace page shows a waterfall nested by parent, with each Span's op, description, start, duration, self time, status and Project. The command palette opens a pasted 32-hex trace id. A trace id from another Organization shows nothing.
- GLOSSARY.md gains Trace, Span, Transaction, Span processing and Trace Retention.

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] Envelopes from sentry-ruby 5.28, sentry-ruby 6.x and sentry-go are answered by the trace route with self time, group, repeats and sample rate. A raw-URL name is stored as Unparameterised, and a retry stores nothing twice (Go, through ingest and the Query API)
- [ ] A Transaction makes no Issue, and the envelope's event and log items are still stored (Go)
- [ ] Spans past trace Retention, and a deleted Project's Spans, are dropped (Go)
- [ ] The trace page's props for a trace across two Projects, with another Organization's trace refused (Rails controller test)
- [ ] A Playwright flow sends an envelope and opens its trace from the palette
