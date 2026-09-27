# Sentry compatibility tests: real SDK traffic and a corpus of real events

Status: ready-for-agent

**Source:** follow-up to `review.md` §6 (fixed in 3ec0486)
**Area:** `event_store/integration_test.go`, `event_store/ingest/`, `event_store/go.mod`

## Problem

Nothing in the repo checks that SolidTrace accepts what real Sentry SDKs send.
Every test builds its payloads by hand. The k6 load tests do this too: the `sentry.ruby`
SDK block in `load_testing/lib/payloads.js` is made up. So the §6 bugs went unnoticed:
multi-item envelopes, transactions, and string timestamps. The payloads matched what the
code expected, not what SDKs send.

Sentry has no official test suite for third-party servers. Relay, Sentry's ingest service,
is the reference implementation. This ticket adds the two cheapest checks that use real
SDK behaviour. Both run in `go test ./...` and need no new toolchain.

## Scope

### 1. sentry-go integration test (transport and envelopes)

`github.com/getsentry/sentry-go` is already in `go.mod` as `// indirect`. Make it a direct
test dependency.

In `event_store/integration_test.go`, next to `TestEnvelopeIngestion`:

- Serve the ingest app on a real port: `net.Listen("tcp", "127.0.0.1:0")`, then
  `app.Listener(ln)`. `app.Test` does not work here, because the SDK sends real HTTP requests.
- Point `sentry.Init` at `http://test_public_key@127.0.0.1:<port>/123`, the seeded key and project
  that `TestEnvelopeIngestion` uses.
- Send the following, then call `sentry.Flush` after each:
  - `CaptureException` with a wrapped error. The event must have an exception and a stack trace.
  - `CaptureMessage`.
  - An event with an attachment. Add it via `scope.AddAttachment`. This makes a multi-item envelope.
  - A transaction. Start one span and finish it. It must **not** create an Issue.
- Assert through the query API or the stores:
  - Each Event arrives.
  - The title is not `Unknown Error` for the exception.
  - The timestamp is within a few seconds of the send.
  - The transaction created no Issue row.

Note: the root package's tests shell out to `bin/rails db:reset` (`review.md` §8), so this
test needs Ruby like the rest of the integration suite. Do not fix that here.

### 2. Corpus test with real events

[bugsink/event-samples](https://github.com/bugsink/event-samples) collects Sentry-compatible
event JSON from many SDKs. It has three sources: Sentry (Apache 2.0), GlitchTip (MIT), and
Bugsink (MIT).

- Check the repo layout first, then copy a representative subset into
  `event_store/ingest/testdata/event-samples/`. Pick at least one sample per platform or SDK.
  Keep each file's license and source attribution. Add a short `README.md` in that directory
  that names the upstream repo and commit.
  Copying the files is better than a git submodule: `go test` must work on a clean checkout.
- Add a table-driven test in `ingest`. It runs each sample through
  `Service.IngestStore` with the existing `fakeIssueRepository`, and asserts:
  - no error;
  - a non-empty title;
  - no `Unknown Error` title when the payload has `exception.values`;
  - an Event timestamp that matches the payload's `timestamp` when one is present,
    not the time of the test run.

Expect some samples to fail. Fix each failure in the classifier or `extractTimestamp`, or
record it as a known gap in this ticket's Comments with the sample name. Do not delete
samples to make the test pass.

Watch for: `IngestStore` (`/store`) does not drop transactions. Only the envelope
path does. If the corpus has `"type": "transaction"` events, decide whether `/store` should
drop them too. It probably should, for the same reason as §6.

## Out of scope (possible later tickets)

- Python and JS SDK scripts. These are the most common SDKs, and Python sends string
  timestamps. They need their own toolchains, so they belong in a CI job.
- Checking payloads against Relay's schema,
  [getsentry/sentry-data-schemas](https://github.com/getsentry/sentry-data-schemas)
  `relay/event.schema.json`. It is useful as the exact reference for field types, and for
  checking the k6 payloads.
- Copying the envelope parser edge cases from Relay's Rust unit tests into
  `service_test.go`: implicit length, trailing newlines, empty payloads, and lengths that run past the end.
- `sentry-cli send-event` / `send-envelope` as a manual smoke test.

## Done when

- Both tests exist and pass in `go test ./...`.
- Every sample failure is fixed or listed as a known gap under Comments.
