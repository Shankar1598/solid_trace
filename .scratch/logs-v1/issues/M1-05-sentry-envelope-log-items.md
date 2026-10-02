# M1-05: Sentry envelope log items

**What to build:** a Sentry SDK with logs turned on gets its Logs stored instead of dropped. The envelope route stores each `log` item's Logs, mapped from Sentry's log format, alongside any `event` item in the same envelope. Spec: [Log ingest](../spec.md#log-ingest-eventstore) (Sources: Envelope; Normalisation: Sentry items); user stories 1, 2, 12, 28.

**Blocked by:** M1-03.

**Status:** resolved

- [x] Each `log` item's log records are normalised and stored as one Log batch; an `event` item in the same envelope is handled exactly as today.
- [x] Sentry mapping: `body` → `message`; `timestamp` (float seconds) → `time`; `level` and `trace_id` fill their fields; typed `{value, type}` attributes unwrapped to plain values; `sentry.environment` fills `environment` and is consumed; other `sentry.*` keys stay Attributes; `service` from a `service.name` attribute. The rest of normalisation (limits, Levels, time rules) applies as for JSON Logs.
- [x] An envelope always answers `200`, even when some of its Logs were dropped, except for overload (`503` with `Retry-After`) as today.
- [x] Tests: an envelope with only a `log` item; one with an `event` and a `log` item (both stored); typed attribute unwrapping; a malformed log record dropped while the envelope gets `200`. Base the fixtures on the real item shape sent by current Sentry SDKs.

## Comments

### 2026-10-02 built (logs-v1/m1-05)

What was built:
- **The Sentry front end** is `normalise.Sentry(record, receipt)` in `ingest/normalise/sentry.go`, beside `normalise.JSON`. It builds the internal `sent` directly and calls `normaliseSent`, so every shared rule (limits, Levels, time window, `original.*`, the reserved prefix, types) applies as for JSON Logs. It does not go through `JSON`, so Sentry attributes named `msg`, `env`, `ts`, `message` and so on stay Attributes.
  - Record fields by name, no aliases: `body` → `message`, `timestamp` → `time`, `level`, `trace_id`, `span_id`.
  - Typed attributes `{"value", "type", "unit"?}` are unwrapped to `value`, then go through `flatten` like any JSON Log's keys. The value's own JSON type decides its map, so a 64-bit integer past a DOUBLE's precision is kept exactly as a string.
  - After flattening, `sentry.environment` fills `environment` and `service.name` fills `service`. Both are consumed. Every other `sentry.*` key stays an Attribute.
- **Envelope ingest** (`ingest/service.go`): `readEnvelope` (was `eventItem`) walks every item and keeps the first `event` payload and every `log` payload. `IngestEnvelope`:
  1. checks the event JSON (`400` as today);
  2. enters the shared gate once for the whole envelope (`503` on overload);
  3. writes the Event exactly as before (`writeEvent`, split out of `ingestEvent`);
  4. normalises the log items' records with `normalise.Sentry` and stores them as one Log batch.
- **Shared write path** (`ingest/logs.go`): `storeLogs(projectID, objects, dropped, normaliseLog)` reads Retention, normalises, gives ids, writes the batch and wakes Log processing. `IngestLogs` and the envelope both use it. `sentryLogRecords` reads each payload's `items` array.
- **Tests:**
  - `logs_sentry_test.go` (Seam 1, four environments, about 13 s), acting as SDKs: a log-only envelope (sentry-ruby framing and records); an `event` and a `log` item in one envelope (sentry-go framing with item lengths, an RFC 3339 timestamp), both stored; typed attributes of every type (sentry-javascript record) listed as plain values; malformed records dropped with a `200`.
  - `ingest/normalise/sentry_test.go`: the mapping, unwrapping, RFC 3339 times, the shared rules, and the precedence rule below.
  - `ingest/service_test.go`: one batch per envelope, Event plus Logs, unreadable records and items, overload, a bad Event storing nothing, and an event item without a payload still `ErrInvalidEventJSON`.

The real item shape, checked 2026-10-02 against the protocol docs and current SDK sources:
- Protocol: https://develop.sentry.dev/sdk/telemetry/logs/ (item header `type`, `item_count`, `content_type: application/vnd.sentry.items.log+json`; payload `{"version"?, "ingest_settings"?, "items": [...]}`; per record `timestamp`, `trace_id`, `span_id`?, `level`, `body`, `severity_number`?, `attributes`; at most one log item per envelope). Attributes: https://develop.sentry.dev/sdk/foundations/state-management/scopes/attributes/ (types `string`, `boolean`, `integer`, `double`, `array`; optional `unit`).
- sentry-ruby 7.1.0: `sentry-ruby/lib/sentry/log_event.rb` (`LogEvent#to_h`: `level`, `timestamp` float, `trace_id`, `span_id`, `body`, `attributes`; no `severity_number`), `utils/telemetry_attributes.rb` (`attribute_hash`), `scope.rb` (`apply_to_telemetry`: `sentry.sdk.name`, `sentry.sdk.version`, `sentry.environment`, `sentry.release`, `server.address`, `user.*`), `telemetry_event_buffer.rb` (`send_items`: logs in their own envelope), `transport.rb` (`serialize_envelope`: lines joined with `\n`, no item `length`).
- sentry-python 2.71.0: `sentry_sdk/_log_batcher.py` (`_to_transport_format`: `timestamp` = ns / 1e9, `sentry.severity_number` and `sentry.severity_text` as attributes), `_batcher.py` (payload `{"version": 2, "items": [...]}`), `utils.py` (`serialize_attribute`), `envelope.py` (`Item.serialize_into`: `length` added, payload followed by `\n`).
- sentry-javascript 11.4.0: `packages/core/src/logs/internal.ts` (record with top-level `severity_number`; `sentry.timestamp.sequence`, `sentry.trace.parent_span_id`), `logs/envelope.ts` (`createLogContainerEnvelopeItem`, `version: 2`, `ingest_settings` in browsers only), `attributes.ts` (integer vs double by `Number.isInteger`, arrays as `array`), `utils/envelope.ts` (`serializeEnvelope`: no item `length`).
- sentry-go 0.49.0: `interfaces.go` (`type Log`: `Timestamp time.Time` with no custom marshaller, so **RFC 3339**, as `interfaces_test.go` asserts), `attribute/value.go` (`mapTypesToStr`; `uint64` is `integer` too), `internal/protocol/envelope.go` (`NewLogItem` sets `length`), `log.go` (`sentry.server.address`, `sentry.message.parameters.N`).

Choices the ticket left open:
- **`timestamp` may be RFC 3339 as well as float seconds.** sentry-go sends a string. The normaliser's time parser already reads both, so nothing Sentry-specific was needed.
- **`span_id` fills its field.** The ticket names only `level` and `trace_id`, but a record's `span_id` is the built-in field. sentry-javascript sends no `span_id`; its `sentry.trace.parent_span_id` stays an Attribute.
- **A record's other top-level fields are Attributes** under their own names, so nothing sent is lost. That is mainly `severity_number` (JS and Go). If an attribute has the same name, the attribute wins.
- **The type and `unit` of a typed attribute are dropped.** The value's JSON type decides its map. An attribute that isn't `{"value": …}`-shaped is kept as sent (flattened if an object). `{"value": null}` stores nothing.
- **One Log batch per envelope**, all log items together, which is the spec's "a request's normalised Logs are one Pebble entry". The protocol allows only one log item per envelope anyway.
- **What is dropped:** a record that isn't a JSON object; every record of a log item whose payload isn't a JSON object with an `items` array (for example a cut-off payload); a Log still over 1 MB. None of this is reported: the envelope answers `200`. Nothing is logged either, as on the JSON route.
- **Order and failure:** event JSON and envelope structure are checked first, so a `400` stores nothing, Logs included (as today, a bad event fails the envelope). The envelope enters the gate once. The Event is written before the Logs. If the Log write or the Retention read fails after the Event was written, the envelope answers `503`/`500` with the Event stored. SDKs don't retry either way.
- **`otel_log` items are not read.** The spec names only `log` items. They are ignored like any other unknown item type.

Deviations: none from the spec.

Follow-ups for later tickets:
- **M1-04:** the envelope route still reads `c.Body()`: Fiber inflates gzip, br and deflate without a cap, and passes zstd through. Sentry SDKs compress envelopes (Relay accepts gzip, deflate, br and zstd), so the envelope is now a Log route too. Decide whether it gets the capped decoding and the raised body limit. A Log `503` from the envelope comes from the same `renderIngestError`, so `Retry-After` lands there once.
- **M1-09:** the envelope reads Retention through the same `storeLogs`, so wiring `storage.ConsoleSettings` covers it.
- An item whose `items` array is cut off loses all its records. A streaming reader could keep the ones before the cut. Not needed for any SDK seen.

