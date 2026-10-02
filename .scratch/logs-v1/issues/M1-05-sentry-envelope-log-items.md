# M1-05: Sentry envelope log items

**What to build:** a Sentry SDK with logs turned on gets its Logs stored instead of dropped. The envelope route stores each `log` item's Logs, mapped from Sentry's log format, alongside any `event` item in the same envelope. Spec: [Log ingest](../spec.md#log-ingest-eventstore) (Sources: Envelope; Normalisation: Sentry items); user stories 1, 2, 12, 28.

**Blocked by:** M1-03.

**Status:** ready-for-agent

- [ ] Each `log` item's log records are normalised and stored as one Log batch; an `event` item in the same envelope is handled exactly as today.
- [ ] Sentry mapping: `body` → `message`; `timestamp` (float seconds) → `time`; `level` and `trace_id` fill their fields; typed `{value, type}` attributes unwrapped to plain values; `sentry.environment` fills `environment` and is consumed; other `sentry.*` keys stay Attributes; `service` from a `service.name` attribute. The rest of normalisation (limits, Levels, time rules) applies as for JSON Logs.
- [ ] An envelope always answers `200`, even when some of its Logs were dropped, except for overload (`503` with `Retry-After`) as today.
- [ ] Tests: an envelope with only a `log` item; one with an `event` and a `log` item (both stored); typed attribute unwrapping; a malformed log record dropped while the envelope gets `200`. Base the fixtures on the real item shape sent by current Sentry SDKs.
