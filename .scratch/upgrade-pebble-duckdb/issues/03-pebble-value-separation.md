# Pebble value separation: benchmark before enabling

Status: ready-for-human

Blocked by: 01

After 01, stores are at `FormatValueSeparation` with the policy off. Values are raw event JSON of about 2.2 KB, which is the case value separation targets.
Before turning on `Experimental.ValueSeparationPolicy`, measure write amplification, `Get` latency (an extra blob read) and space. Also confirm that blob files work with shared-storage tiering (`CreateOnShared`).
Consider running this together with the memtable benchmarks in [prod-readiness 011](../../prod-readiness/issues/011-pebble-memtable-size.md).

## Comments

### 2026-10-18 benchmarked

Benchmarked on `feature/pebble-value-separation`. The results and the source lines are in the [research note](../research/pebble-value-separation.md). The benchmark is `event_store/bench/pebblevalsep/`. The recommendation is **not to turn it on now**. Whether to turn it on is the user's call, so this is `ready-for-human`.

Setup: 1 GB of 2.2 KB Event JSON, written as `WriteEvent` does, with `storage/pebble.go`'s options. Off is compared with on at `MinimumSize` 512, which separates every Event.

- **Write amplification:** what flushes and compactions write over the data's life falls from 1.4× to 0.5× of the data written, or from 2.4× to 1.5× with the WAL. It is low already, because Event keys only grow.
- **Write throughput** at saturation doesn't change: 40–48k Events/s either way. Memtable flushes bound it, not compactions.
- **`Get`:** a cold `Get` of a compacted Event is 25–55% slower at p50, for example 742 µs against 479 µs after a full compaction. Warm `Get`s are about the same.
- **Space:** the store is 28% bigger, 500 MB against 389 MB. Blob files keep the MinLZ compression of L0, where flushes write them. Without separation, Events end in L6 as zstd. zstd for L0–L4 removes the extra space, but costs 20–30% of saturation throughput.
- **`CreateOnShared`:** Pebble v2.1.7 never writes blob files to shared storage. Flushes and compactions whose output goes to shared levels write the values back into their sstables (`compaction.go:607-617` and `865-873`). Blob file rewrites stay local (`blob_rewrite.go:63`, a TODO).
  - With `CreateOnSharedLower` (a tier at `Level` > 0), separation applies only to L0–L4. With `CreateOnSharedAll` (`Level` 0), it never happens.
  - Pebble gives no error or warning, and reads after a reopen work.
- **If it is turned on later:** `MinimumSize` 512, `MaxBlobReferenceDepth` 10, `RewriteMinimumAge` 5 min. Set `TargetGarbageRatio` below 1 (for example 0.2), because Log batches share blob files with Events and are deleted.

For the user: close as `wontfix`, or keep it open for when disk writes or a much larger store make it worth it.
