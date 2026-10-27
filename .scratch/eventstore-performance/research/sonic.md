# sonic against EventStore's JSON

Asked for by the user on 2026-10-27, after [02](../issues/02-go-1-27-without-slower-json.md) moved EventStore to Go 1.27 and the v2 API: should EventStore use [bytedance/sonic](https://github.com/bytedance/sonic)?

**Decision: not now.** Sonic is 30–40% faster on every JSON path EventStore has, but switching costs a behaviour change, a rewrite of the Attributes encoder, and a dependency that must catch up with every Go release. Take it up only if production's ingest is bound by CPU. If it is, put sonic's `ConfigStd` on the Log body path alone (`ingest/logs.go`, `normalise`) and keep Events and answers on the v2 API.

## Figures

Sonic v1.15.5 on Go 1.27.3, dev box (AMD EPYC 7B13), `-cpu 2`, 5 rounds of 300 ms, medians. Rounds varied by 20–60%, so treat these as direction, not exact figures. The fixtures are the repo's benchmarks' fixtures. The code is in [sonic-bench/](sonic-bench/), and the raw output is `sonic-bench/bench.txt`. Run it in under 20 s:

```
cd .scratch/eventstore-performance/research/sonic-bench
go test -run '^$' -bench . -benchmem -benchtime 300ms -count 5 -cpu 2 .
```

| Path | Today | sonic `ConfigStd` | sonic `ConfigDefault` |
|---|---|---|---|
| Log body: `Valid`, split the array, decode 100 Logs and their nested objects into `map[string]RawMessage` | 2.68 ms | 1.60 ms (−40%) | 1.20 ms (−55%) |
| Parse a 4 KB Sentry Event into `map[string]any` (today: `jsonv2.Unmarshal`) | 61 µs | 44 µs (−29%) | 40 µs (−34%) |
| Encode a page of 200 Logs | 1.68 ms | 1.16 ms (−31%)* | 0.84 ms (−50%)* |

\* Not a like-for-like comparison. Sonic ignores `Attributes.MarshalJSONTo` (a v2 interface), so it was given the same page with Attributes as one plain map. Switching would mean rewriting that encoder for sonic.

The Log body's JSON is about half of ingest's work: 5.4 ms per 100 Logs in issue 02's `LogBodyToLogs/array`. Sonic `ConfigStd` would save about 1 ms per 100 Logs, roughly 20% of ingest's CPU.

## Behaviour on the inputs issue 02 pinned down

| Input | encoding/json (today) | sonic `ConfigStd` | sonic `ConfigDefault` |
|---|---|---|---|
| Duplicate name | last value wins | same | same |
| Invalid UTF-8 | U+FFFD | same | **raw byte kept** |
| Number too big for float64 | float64 | same | same |
| Nesting depth | up to 10,000 | **up to 4,096** | **up to 4,096** |

`ConfigDefault` is out because it keeps invalid UTF-8. With either mode, Logs nested between depths 4,096 and 10,000 would be dropped, and an existing root test pins 10,000.

## Other costs

- Sonic generates machine code at runtime and reaches into Go's runtime internals through `linkname`, so each Go release needs a sonic release to match. Its README still says to avoid Go 1.24.0, which broke it.
- The README says that where sonic can't run, it falls back to `encoding/json`. A build on an unsupported setup would quietly lose the gain.
- `ConfigDefault`'s answers differ from today's: it doesn't escape HTML or sort map keys. `ConfigStd` does both. Nobody has diffed sonic's answers byte for byte against today's.
