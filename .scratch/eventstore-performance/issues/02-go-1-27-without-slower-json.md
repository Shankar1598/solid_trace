# 02: Go 1.27, with JSON no slower than on 1.25

**Status:** ready-for-agent

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

- [ ] A benchmark of the JSON ingest path, from a request body to normalised Logs, and of encoding a page of Logs, run on 1.25 and on 1.27 (under a minute). Figures are in this ticket's Comments.
- [ ] On 1.27, neither is slower than on 1.25. The hot paths call the v2 API, or the build sets `nojsonv2`. Say which and why in the Comments.
- [ ] Ingest accepts and rejects the same bodies as before: duplicate names, invalid UTF-8, numbers too big for float64, deep nesting. Root tests through the ingest routes.
- [ ] Go 1.27 in `mise.toml`, `go.mod` and the Dockerfile. `go vet` is clean, and the targeted ingest, normalise, storage and Query tests pass. The user runs the full suite.

**Blocked by:** None.

## Comments
