# M1-04: Log body rules and overload

**What to build:** shippers can send compressed and large batches to the Log routes, and every overloaded ingest tells the sender when to retry. Spec: [Log ingest](../spec.md#log-ingest-eventstore) (Body, Overload); user stories 8, 9, 13.

**Blocked by:** M1-01.

**Status:** resolved

- [x] Log routes accept `gzip`, `zstd` or identity bodies and decompress in EventStore, not through Fiber's pass-through.
- [x] Over 10 MB compressed or 50 MB decompressed, the route answers `413` and stores nothing; decompression stops at the cap instead of inflating the whole body.
- [x] Body limits are raised for Log routes only, in EventStore's server; Event routes keep today's limits. (Angie is removed by [M1-11](M1-11-remove-angie.md), so there is no proxy limit to raise.)
- [x] Every ingest `503` (overload or shutting down), Event routes included, carries a `Retry-After` of a few seconds. No `429`.
- [x] Tests: gzip and zstd bodies stored; `413` on each cap with nothing stored; `Retry-After` on an Event `503` and a Log `503`.

## Comments

### 2026-10-02 built (logs-v1/m1-04)

What was built:
- **Capped decoding** is `requestBody(c, limit)` in `event_store/handler/body.go`. It reads the raw body (`c.Request().Body()`), never `c.Body()`, and decodes its `Content-Encoding` with a streaming reader read through `io.LimitReader(50 MB + 1)`. So decoding stops at the cap. `renderBodyError` maps the result to a status:
  - **413:** over the route's cap as sent, or over 50 MB decoded.
  - **415:** an encoding EventStore doesn't decode.
  - **400:** a body that doesn't decode as its encoding says (corrupt or cut short).
- **Every ingest route uses it.** That is the Log route, the envelope route (it carries Logs since M1-05) and the store route. No ingest route inflates a body without a bound any more. The handler decodes after auth, so an unauthenticated request costs no decompression.
- **Caps** (binary MB, like the normalisation limits):
  - The Log route takes 10 MB as sent.
  - The store and envelope routes take 4 MB as sent. That is Fiber's default, which they had before.
  - Every route takes 50 MB decoded.
- **The body limit is raised for the Log route only.** `handler.NewIngestApp(ingestHandler, healthHandler)` builds the ingest app with `BodyLimit` 10 MB. fasthttp then refuses a larger body itself, with `413`, before reading it. The store and envelope routes refuse more than 4 MB in their handlers.
  - `main.go` and the test environment both build the ingest app with `NewIngestApp`, so the tests run against the production limits.
  - M1-11's `ReadTimeout`/`IdleTimeout` moved from `main.go` into `NewIngestApp` unchanged.
- **Retry-After:** `renderIngestError` sets `Retry-After: 5` on every ingest `503` (`ErrOverloaded` or `storage.ErrShuttingDown`), for the Event routes, the envelope and the Log route alike. It still never sends a `429`.
- **Tests** (Seam 1, `event_store/logs_body_test.go`, six tests, about 18 s):
  - gzip, zstd and identity Log bodies are stored;
  - `413` with nothing stored for identity over 10 MB, gzip over 10 MB as sent, and gzip or zstd over 50 MB decoded;
  - a 6 MB body is taken by the Log route but gets `413` on the store and envelope routes;
  - envelopes in gzip, br, deflate, zstd and sentry-ruby's empty `Content-Encoding` are stored, and so is a gzip Event on the store route;
  - `413` for an envelope or Event over 50 MB decoded;
  - `415` for an unknown or stacked encoding and `400` for a broken one, on all three routes.

  The two existing writer-stopped tests now also check `Retry-After` on an Event `503`, a Log `503` and an envelope `503`. `TestRenderIngestErrorStatus` checks `Retry-After: 5` on both `503` errors, and no `Retry-After` on any other status. The old `415` test (`TestCompressedLogBodyIsRefused`) is gone.

Choices the ticket left open:
- **The envelope keeps the Event cap as sent (4 MB), not the Log cap (10 MB).**
  - Sentry SDKs send log envelopes of at most about 100 records. sentry-ruby flushes at 100 (`LogEventBuffer::DEFAULT_MAX_EVENTS`) and sentry-javascript at 100 (`MAX_LOG_BUFFER_SIZE`). Such envelopes are typically far under 4 MB, even uncompressed.
  - Raising the cap would mostly admit bigger attachments, which EventStore drops anyway.
  - The envelope does get the 50 MB decoded cap, which it never had.
- **Encodings: identity, `gzip` (and `x-gzip`), `deflate` (the zlib format, as fasthttp read it), `br` and `zstd`, on every ingest route.** The spec lists gzip, zstd and identity for Log routes. These are a superset, and one decoder serves all three routes.
  - `br` has to stay on the envelope. sentry-python 2.x sends `Content-Encoding: br` by default whenever the `brotli` module is installed (`transport.py`: `"gzip" if compression_level is not None or brotli is None else "br"`). Fiber's `c.Body()` decoded br, so dropping it would have broken Event ingest for those SDKs.
  - `deflate` stays for the same reason: `c.Body()` decoded it.
  - An empty or `identity` encoding is identity. sentry-ruby sends `Content-Encoding: ""` when it doesn't compress.
- **Only one encoding per body.** A body that names two (for example `gzip, zstd`) gets `415`, rather than being decoded layer by layer. Fiber decoded up to three, in the wrong order per RFC 9110. No SDK or shipper stacks encodings, and layering would multiply the decoding work an attacker can ask for.
- **Unknown encodings now get `415` on the Event routes too.** Before, Fiber passed them through, and the store or envelope route answered `400` because the body didn't parse. zstd envelopes and Events, which used to be passed through and refused with `400`, are now decoded.
- **The zstd decoder** runs synchronously (`WithDecoderConcurrency(1)`), and `WithDecoderMaxMemory(50 MB)` makes it refuse a frame whose window is over 50 MB. Such a frame gets `413`.
- **`Retry-After` is 5 seconds**, the same for overload and for shutting down. Of the Sentry SDKs, only sentry-javascript reads `Retry-After` on a non-429 (`updateRateLimits`): it pauses sending for those 5 s. sentry-python, sentry-ruby and sentry-go read it only on a `429`, so for them a `503` still drops just that request.
- **A test helper reads fasthttp's own 413.** When the server refuses a body over the app's 10 MB limit, it writes `413` and closes the connection, and Fiber's `app.Test` reports that as `fasthttp.ErrBodyTooLarge` instead of a response. `sendEncoded` (in `logs_body_test.go`) turns that error into a `413`, with a comment explaining why.
- `go.mod` now lists `github.com/andybalholm/brotli` and `github.com/valyala/fasthttp` as direct requirements. They were indirect before, at the same versions, and `go.sum` is unchanged.

Checked with a throwaway probe (not committed, under the 3 GB cap): a zstd bomb of 2 GiB of zeros (426 KB sent) and a gzip bomb of 1 GiB (1 MB sent). Each got `413` in 0.2 to 0.35 s on all three routes. Peak memory was 824 MiB, the same as the test environment's baseline.

Deviations from the spec:
- The spec lists gzip, zstd and identity; the routes also take `deflate` and `br` (see above).
- The envelope is a Log route for decoding and the 50 MB decoded cap, but not for the raised 10 MB cap as sent.

Follow-ups for later tickets:
- **M1-10 (load test):** decoding happens before the `ingest_max_waiting` gate, so nothing bounds how many bodies are decoded at once. Each decode holds up to 10 MB as sent plus 50 MB decoded. A burst of large compressed batches could use a lot of memory before the gate turns any away. Measure it under load. If it matters, a small semaphore around `requestBody` would bound it.
- **Console:** still has no request-body cap behind Thruster (M1-11's follow-up). This ticket covers EventStore only.

