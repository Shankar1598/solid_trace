# 01: Probe: VRL through cgo, timed

**What to build:** EventStore can compile a VRL program and run it on a batch of Logs, through the Rust `vrl` crate built as a staticlib and called over cgo, with Logs crossing as msgpack. A probe under a minute times a realistic program on real `sidekiq.log` job lines, which decides whether the rest of the [spec](../spec.md) goes ahead on this runtime. See the spec's "The VRL runtime" and "Order of work".

**Blocked by:** None (can start immediately)

**Status:** claimed

- [ ] A narrow Go interface: compile a program into a compiled program or diagnostics (severity, code, message, labels with byte start and end, VRL's formatted text), and run a compiled program on a slice of Logs in one call, returning one outcome per Log: the returned value, dropped by `abort`, or failed with the error text.
- [ ] The crate is built without default features: the stdlib, but no network, environment or system functions. A program calling `http_request` or `get_env_var` gets VRL's undefined-function compile error.
- [ ] A panic inside VRL is caught at the boundary and becomes a failed outcome for that Log; the process carries on.
- [ ] A compiled program can be freed.
- [ ] Local development gets Rust through `mise`, and one documented step builds the staticlib before `go build` and `go test`.
- [ ] The probe runs in under a minute on the dev box: through the cgo bridge, batched, a program that `parse_regex`es a `sidekiq.log` job line taken from the `tagalys` Project, sets four Attributes and deletes `message`. It reports microseconds per Log including msgpack both ways.
- [ ] Pass: about 75 µs per Log or less (about 3,300 Logs/s in under a quarter of one core). If it passes, an ADR records VRL through cgo, run in Log processing, with the probe's numbers and the alternatives (WASM with wazero, OTTL, expr and CEL, running at ingest).
- [ ] If it fails, stop: record the numbers here and hand the runtime question back to the user. Build nothing further.
