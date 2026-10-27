# 02: Go 1.27, with JSON no slower than on 1.25

**Status:** resolved

**What to do:** Move EventStore from Go 1.25 to Go 1.27 (1.27.3 is the newest, 2026-10-21), and keep its JSON at least as fast as today. Asked for by the user on 2026-10-21: "we cannot let json get slower".

## Why JSON could get slower

From [performance-ideas.md](../research/performance-ideas.md), section 11–13 (cited from the web, not measured here):

- In Go 1.27, `encoding/json/v2` graduated, and the v1 API (`encoding/json`) now runs on the v2 engine.
- Decoding into `map[string]any`/`any` through the v1 API was measured 15–48% slower on Apple silicon. The v2 API itself was 1.76× faster than v1 on `any`. Neither study measured `map[string]json.RawMessage`, which ingest uses.
- Two ways to keep it fast:
  - call the v2 API (`encoding/json/v2`, `encoding/json/jsontext`) on the hot paths;
  - build with `GOEXPERIMENT=nojsonv2`, which keeps the old engine but may go away in a later release.
- The v2 API rejects duplicate object names and invalid UTF-8 by default, where v1 accepted them. Options such as `jsontext.AllowDuplicateNames` and `jsontext.AllowInvalidUTF8` keep v1's behaviour.

## Where EventStore uses JSON

Today it uses std `encoding/json` everywhere (the report's table has file:line pointers):

- Log ingest: `ingest/logs.go` (`json.Valid`, the array, each Log's object), `ingest/normalise/` (`flatten`, `parseString`, `json.Compact`, Sentry's `unwrap`, the Log transform's round trip);
- answers through Fiber v2's `c.JSON` (`json.Marshal`), and `models.Log`'s `Attributes.MarshalJSON`.

## Where Go is pinned

- `mise.toml` (`go = '1.25'`), `event_store/go.mod` (`go 1.25.5`), and `event_store/Dockerfile` (`golang:1.25-bookworm`).
- Check that the VRL cgo build, duckdb-go and Pebble build and pass on 1.27, on amd64 here and arm64 in the release workflow.

- [x] A benchmark of the JSON ingest path, from a request body to normalised Logs, and of encoding a page of Logs, run on 1.25 and on 1.27 (under a minute). Figures are in this ticket's Comments.
- [x] On 1.27, neither is slower than on 1.25. The hot paths call the v2 API, or the build sets `nojsonv2`. Say which and why in the Comments.
- [x] Ingest accepts and rejects the same bodies as before: duplicate names, invalid UTF-8, numbers too big for float64, deep nesting. Root tests through the ingest routes.
- [x] Go 1.27 in `mise.toml`, `go.mod` and the Dockerfile. `go vet` is clean, and the targeted ingest, normalise, storage and Query tests pass. The user runs the full suite.

**Blocked by:** None.

## Comments

### 2026-10-21: built on `feature/go-1-27`, merged into `main`

Go 1.27.3 in `mise.toml`, `go.mod` and the Dockerfile (`golang:1.27-bookworm`, published for amd64 and arm64). The build uses Go 1.27's default JSON engine, and three hot paths call the v2 API. Nothing measured is slower than on 1.25.

**Figures.** Dev box (AMD EPYC 7B13), `-cpu 2`, 8 interleaved rounds per side, benchstat p < 0.01 unless marked. Go 1.25.14 runs the old code, and Go 1.27.3 runs the new.

| Benchmark | Go 1.25 | 1.27, code unchanged | 1.27 with the changes | vs 1.25 |
|---|---|---|---|---|
| `LogBodyToLogs/array` (100 Logs, 87 KB) | 7.52 ms | 5.47 ms | 5.42 ms | −28% |
| `LogBodyToLogs/ndjson` | 6.08 ms | 5.13 ms (n.s.) | 4.99 ms | −18% |
| `LogBodyToLogs/sentry` | 5.06 ms | 3.75 ms | 3.58 ms | −29% |
| `TransformedLogs` (100 Logs) | 6.30 ms | **6.87 ms, +11%** | 6.16 ms | no change (n.s.), −22% bytes |
| `EncodeLogPage` (200 Logs) | 3.24 ms | 3.28 ms, +54% bytes | 1.28 ms | −60%, −97% allocations |
| `ParseEvent` (4 KB Sentry Event) | 73.4 µs | **104.6 µs, +42%** | 57.6 µs | −22% |
| `json.Valid` on that Event | 19.2 µs | 5.0 µs | 5.0 µs | −74% |

Run them with `go test -run '^$' -bench . -benchmem -count 6 ./ingest/ ./models/ ./processing/`. The script that compares two checkouts on two toolchains is `~/.cache/solid-trace/eventstore-performance-notes/json-bench.sh` (outside the repo). Raw outputs are in `bench/run1` to `run5` beside it.

**What was slower on 1.27, and the fix:**
- `normalise.Transformed` built a v1 `Encoder` for each value. It now calls `jsonv2.Marshal` with `json.DefaultOptionsV1()` and `EscapeForHTML(false)`, so the JSON is the same as before.
- Event processing parsed each Event into `map[string]interface{}`, the case the research flagged. Allowing duplicate names is what makes the v2 engine slow there: 90 µs with them allowed against 52 µs with them rejected. `parseEvent` tries `jsonv2.Unmarshal` with v2's defaults, and if v2 rejects the Event (duplicate names, invalid UTF-8), parses it again with `encoding/json`, which accepts it as before.
- `Attributes.MarshalJSON` built a map for each Log. `MarshalJSONTo` writes names sorted and each value straight to the encoder. `json.Marshal` and Fiber's `c.JSON` call it.

**Why not `nojsonv2`:** it keeps 1.25's speed exactly (measured), but gives up ingest's 18–29% and `json.Valid`'s 74%, and Go may remove it. Under it `encoding/json/v2` doesn't exist, so none of the fixes above could be made.

**Same bodies accepted and rejected.** New root tests: a duplicated name keeps its last value (Logs and Events); invalid UTF-8 becomes U+FFFD (Logs and Events, in values and names); numbers a DOUBLE can't hold exactly are stored as text; a Log nested past depth 10,000 is dropped. Each passed on Go 1.25 with the old code before the change, and passes on 1.27.

**One byte-level change in answers.** Go 1.27's engine writes an invalid byte as the U+FFFD character itself, where 1.25 wrote the escape `�`. Any JSON reader gets the same string. Everything else a page of Logs encodes was diffed byte for byte across the two builds and is identical: HTML escaping, U+2028, number forms, name order, JSON values.

**GODEBUG.** Moving `go.mod`'s go line to 1.27 changes no default that EventStore relies on. `urlstrictcolons` rejects only malformed hosts in a bucket endpoint.

**Checked:** `go vet ./...` is clean, and `go mod tidy` changes nothing. These pass on 1.27: the `ingest`, `normalise`, `models`, `processing`, `storage`, `handler` and `vrl` packages, the 61 root tests in `logs_test`, `logs_sentry_test`, `logs_transform*_test`, `logs_query_test`, `logs_normalise_test` and `logs_body_test`, and the Event processing root tests.

**Not checked:**
- the Docker image build;
- the arm64 build, which the next release's workflow tries first;
- the full Go suite, which is for the user.

`.vscode/settings.json` points `go.goroot` at mise's `1.27` link (changed on the merge, at the user's request).

**Left on the v1 API, not hot:** the SQL read-only check (`storage/logs_sql_readonly.go`, once per SQL query) and the Log transform preview (`logtransform/view.go`).

### 2026-10-27: sonic measured, not adopted

[bytedance/sonic](https://github.com/bytedance/sonic) v1.15.5 is 30–40% faster than these paths on Go 1.27 (`ConfigStd`). It isn't adopted: it caps nesting at depth 4,096, ignores `MarshalJSONTo`, and has to catch up with each Go release. Figures, behaviour and when to revisit are in [research/sonic.md](../research/sonic.md).
