# 04: Check, preview and stats routes

**What to build:** EventStore keeps each Project's recent arrivals and the current version's running counts in memory, and serves the three loopback routes Console and the MCP tools need: check a program, preview a draft without storing anything, and read the running counts. See the spec's "Recent arrivals and running counts" and "EventStore's loopback routes".

**Blocked by:** 02

**Status:** resolved

- [ ] Recent arrivals: per Project, the newest Logs as they arrived (before any program), up to 100 per Service and at most 4 MB per Project, oldest first out. Log processing fills them as it reads batches.
- [ ] Running counts per Project for the current version: processed, dropped, failed and total program time; they reset when the version changes; a version that doesn't compile reports its compile error.
- [ ] `POST /api/:project_id/logs/transform/check` takes `{ program }` and returns `{ errors, warnings }`, each with `message`, `code`, `start`, `end` (byte offsets) and `text`. A program over 64 KB is an error.
- [ ] `POST /api/:project_id/logs/transform/preview` takes `{ program, logs? }`, runs on the recent arrivals or on up to 200 given JSON Logs (normalised as Log ingest normalises them first), and returns each result as `{ before, after }`, `{ before, dropped: true }` or `{ before, error }`, with `kept`, `dropped` and `failed`; or the check's `errors` when it doesn't compile. Nothing is stored.
- [ ] `GET /api/:project_id/logs/transform/stats` returns `{ version_id, processed, dropped, failed, average_micros, compile_error? }`.
- [ ] Root tests through the routes: check reports spans; preview runs on recent arrivals per Service and on given Logs and stores nothing; stats count and reset on a new version.

## Answer

Built and merged (`a4e7e0b`, 2026-10-21). `logtransform`: `CheckProgram`, recent arrivals (filled in `Apply` before the program, once per batch), `Transforms.Preview` sharing `runProgram`/`stored` with Log processing, running counts as atomics on each version. Routes in `handler/log_transform.go`. Shapes: check `{errors, warnings}` of `{message, code, start, end, text}` (over 64 KB: code 0, "the program is over 64 KB"); preview `{results, kept, dropped, failed}` with `{before, after}`, `{before, dropped: true}` or `{before, error}` where a Log is `models.Log` JSON as in query results, or the check's `{errors, warnings}` with 200 when it doesn't compile, 400 `{error}` for over 200 Logs, a non-object or a Log over 1 MB; stats `{version_id (null with no transform), processed, dropped, failed, average_micros, compile_error?}`. Decisions: recent arrivals preview newest first; empty `logs` previews recent arrivals; given Logs get receipt time now; stats read Console's current version on each call and never free the cached program; `processed` includes dropped and failed. Tests: `logs_transform_routes_test.go` (10).
