# 03: Sentry envelope log items become Logs

**What to build:** Structured logs sent by current Sentry SDKs (envelope items of type `log`) are stored as Logs for the Project identified by the envelope's key, with attributes typed from the SDK's `{value,type}` encoding and well-known keys mapped to columns. Existing error events in the same envelope keep working.

**Blocked by:** 01

**Status:** wontfix

- [ ] The envelope reader iterates every item, honouring the item `length` header and falling back to next-newline, and dispatches `event`/`transaction` to the existing path and `log` to Log ingest (shared with, not duplicated from, the multi-item fix tracked in prod-readiness)
- [ ] Log item payload `{"items":[...]}` maps timestamp, trace_id, span_id, level, body, severity_number and attributes; `sentry.environment`, `server.address` and `sentry.sdk.name` populate environment, host and service
- [ ] An envelope containing only a log item returns 200 and creates no Issue
- [ ] Verified in dev with `sentry-ruby` 7 (`Sentry.logger.info` and a formatted message with parameters)
- [ ] Adapter tests cover typed attributes, missing optional fields and a mixed event+log envelope

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
