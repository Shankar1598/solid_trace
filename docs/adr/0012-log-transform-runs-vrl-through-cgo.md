# The Log transform runs VRL through cgo, in Log processing

A Project's Log transform is a VRL program, the language of Vector's transforms. EventStore runs it with the Rust `vrl` crate, built as a static library and called over cgo, in its `vrl` package. Log processing runs it after a Project's Log batches are read from Pebble and before the hot/late split, on a slice of Logs per call, with the Logs crossing as msgpack. Ingest is unchanged: its `200` still means the Logs are stored as they arrived.

A future reader may wonder why a Go service links a Rust library, and why the program doesn't run at ingest. Both are this decision. A probe decided it: the spec's sidekiq program (`parse_regex` on a `sidekiq.log` job line, four Attributes set, `message` deleted), on Logs shaped as Log processing passes them, through the bridge and msgpack both ways (`BenchmarkSidekiqProgram` in `event_store/vrl/`). The pass mark was about 75 µs a Log: 3,300 Logs/s in under a quarter of one core. On the dev box (x86_64, AMD EPYC 7B13, one core, 500 Logs a call):

| Logs | µs a Log |
|---|---|
| Real job lines (124 B a `message`) | 9 |
| A mix with longer lines, 292 B a `message` as in production | 7–9 |
| The mix, one Log a call | 12 |

Production is arm64 (n4a, 2 vCPU) and wasn't measured; the margin is about eight times.

**What the build leaves out:**
- `vrl` 0.37.0 is pinned exactly and built without default features, with `stdlib-base` and `enable_crypto_functions` on. That leaves out the network, environment and system functions (`http_request`, `dns_lookup`, `get_env_var`, `get_hostname` and their kin), so a program calling one gets VRL's E105, "call to undefined function".
- `decode_gzip`, `decode_zlib`, `decode_zstd` and `decode_snappy` are left out by name. They have no cap on their output, so a small Log could make them exhaust memory.

**How it runs:**
- Only `abort` drops a Log. Any other termination fails the Log with VRL's error text, as does a program that leaves `.` as something other than an object.
- Panics unwind and are caught at the boundary: a panic fails its Log, and the next Log gets a fresh runtime.
- Programs run in UTC, never the host's time zone.
- Programs compile against VRL's default types, every field `any`, as in Vector. A program written for Vector (`string!(.message)`, `parse_regex!(...)`) compiles unchanged.

## Considered options

- **VRL compiled to WASM, run by wazero:** no cgo and no Rust in the Go build, and a crash stays in the sandbox. But the crate's C dependencies (Oniguruma, for grok) need a WASI C toolchain, every Log is copied into the module's memory, and the code runs slower than native. The native probe passed with room to spare, so the sandbox wasn't worth the toolchain.
- **OTTL, the OpenTelemetry Collector's language:** pure Go, but its functions work on the Collector's own Log types, so every Log would be converted there and back. Fewer users have programs written in it to paste.
- **expr or CEL:** pure Go, but they evaluate one expression to a value. Statements that set, delete and drop fields, and a library like VRL's `parse_*` functions, would be ours to design and keep.
- **Running at ingest:** Log processing would stay as it is. But the program's cost would sit in front of every sender's `200`, and ingest would gain a way to fail. In Log processing, ingest stays as fast as it is, and a slow program only delays when Logs reach the hot table.

## Consequences

- EventStore's builds need Rust (1.95 or later, through `mise`) and a C compiler. `event_store/vrl/build` builds the library before `go build` or `go test`; the first build takes about four minutes and 1.3 GB of memory with two jobs, and the library is 75 MB. EventStore's Docker image builds it in a Rust stage, with fat LTO (the crate's `dist` profile): for about the same memory, the library is 31 MB and the binary 10 MB smaller.
- The build step also writes `libhash_gen.go` with the library's hash. Go's build cache keys cgo on the link flags' text, not the library, and would otherwise reuse a binary linked against an older one.
- The link adds `-z noexecstack`: `psm`'s assembly has no GNU-stack note, which would otherwise give EventStore an executable stack.
- A stack overflow or a failed allocation inside VRL can't be caught, and stops EventStore. A Log nested more than 64 levels deep fails before VRL sees it, and the unbounded decoders are left out.
- Upgrading `vrl` can change what compiles. A saved version that no longer compiles stores every Log of the Project as it arrived, with the compile error, so nothing is lost.

Source: [.scratch/log-transform/spec.md](../../.scratch/log-transform/spec.md)
