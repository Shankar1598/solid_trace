# Pebble value separation for Event payloads

2026-10-18, for [ticket 03](../issues/03-pebble-value-separation.md). Should EventStore turn on `Experimental.ValueSeparationPolicy`? Run on the 4-CPU, 11 GB dev box with Pebble v2.1.7, the version `event_store/go.mod` pins.

The benchmark is `event_store/bench/pebblevalsep/` (`valsep_test.go`, `gen_test.go`, `gen_check_test.go`). Each test skips unless `PEBBLE_VALSEP_BENCH=1`; each run takes under a minute. The package comment has the commands.

## Answer

- **Don't turn it on now.** For Events it costs more than it saves.
- **Write amplification is already low.** Event keys only grow (a UUIDv7 under each Project), so L0 files don't overlap and many compactions are moves. Separation cuts what flushes and compactions write over the data's life from about 1.4× to 0.5× of the data written. With the WAL, that is about 2.4× to 1.5×. Disk write bandwidth is not a limit at real ingest rates.
- **Write throughput doesn't change.** At saturation the write path waits on memtable flushes, not on compactions. Every run stalled 15 times, with the policy off or on.
- **Cold reads of older Events get slower.** A `Get` that misses the caches reads the sstable, then the blob file: p50 rises by 25–55%.
- **The store gets bigger.** Blob files are compressed for the level that writes them, and flushes write them into L0, so they stay MinLZ. Without separation, Events end in L6 as zstd. The store is 28% bigger (500 MB against 389 MB). zstd for L0–L4 fixes the space, but slows flushes: saturation throughput falls by 20–30%.
- **It does nothing on shared storage.** Pebble v2.1.7 never writes blob files to a shared tier. A flush or compaction whose output goes to shared storage writes values back into its sstables. Pebble neither refuses nor warns. Reads work after a reopen.

## Setup

- **Options:** the same as `storage/pebble.go`: `FormatValueSeparation`, a 64 MB memtable, `LBaseMaxBytes` 64 MB, `L0CompactionThreshold` 4, `L0StopWritesThreshold` 12, `DBCompressionBalanced`. Everything else is Pebble's default: an 8 MB block cache and one compaction at a time.
- **Writes:** like `PebbleWriter.WriteEvent`. Four goroutines stand in for ingest requests. Each write takes a mutex, makes a UUIDv7 key, and commits one batch with the Event key, with `NoSync`. Ticket [011](../../prod-readiness/issues/011-pebble-memtable-size.md) also wrote an Event log key. `WriteEvent` no longer writes one, so the benchmark doesn't.
- **Keys:** the Project id (4 bytes) and the UUIDv7 (16 bytes), over four Projects that get 55%, 25%, 15% and 5% of Events.
- **Values:** Sentry-style Event JSON, with an exception, 1–7 stack frames, 0–4 breadcrumbs, tags, user, request, contexts and extra fields. 8,192 payloads are generated, and each write gives its copy a new `event_id`.
  - Mean 2,247 B, p10 1,759 B, p90 2,761 B, max 3,765 B. 65% are 2 KB or more.
  - zstd (fastest) on 32 KB runs of them: 5.6×.
- **Volume:** 1 GB of keys and values, about 474,000 Events, written as fast as possible (about 11 s). Then a flush, and a wait until no compaction has run for 2 s.
  - Pebble's estimated compaction debt never reaches zero here. L0's files don't overlap, so they form one sublevel and L0's score stays at 0.5 (`compaction_picker.go:1075-1097`). Pebble leaves them in L0 until two sublevels overlap or there are 500 files (`L0CompactionFileThreshold`).
- **Full compaction:** `TestCompact` then compacts the whole store into L6, where years of writes would take most Events. This stands in for the compactions the rest of the data's life would cost.
- **Policies:**
  - `off`: the current setting.
  - `on`: `MinimumSize` 512, `MaxBlobReferenceDepth` 10, `RewriteMinimumAge` 5 min, `TargetGarbageRatio` 1.0, the settings of Pebble's own benchmark tool (`cmd/pebble/db.go:85`). Every Event is separated.
  - `on2k`: the same, with `MinimumSize` 2048.
  - `onz`: `on`, with the Balanced compression profile for L0–L4 too, so blob files are zstd.
- **Write amplification:** the bytes flushes and compactions wrote (sstables and blob files) over the bytes of keys and values.
  - Pebble v2.1.7 counts a flush's blob files twice, in `BlobBytesFlushed` and `BlobBytesCompacted` (`compaction.go:3451-3454`). The benchmark takes them out once, so Pebble's own `WriteAmp()` overstates separation's.
- **Space:** live sstables plus live blob files.
- **Cold reads:** the store is closed, its files are dropped from the page cache (`posix_fadvise` `DONTNEED`), and it is reopened. 1,000 `Get`s each, on the box's persistent disk.
- **Warm reads:** every file is read once into the page cache, then the store is reopened with the 8 MB block cache. 50,000 `Get`s each.
- **Recent and old:** "hot" is the newest 2,000 Events, read once first. "Recent" is the newest 5%, and "old" is the oldest half.
- **Scan:** 100,000 Events of the busiest Project read in key order, like `EventsAfter`, warm.

## Results

Two runs each for writes and reads; ranges show both. The `onz` column is one run.

| | off | on (512 B) | on2k (2 KB) | onz (zstd blobs) |
|---|---|---|---|---|
| Write throughput at saturation | 40.4k–44.0k Events/s | 42.4k–47.9k | 43.4k–44.3k | 32.7k |
| Write stalls in an 11 s run | 15, 3.9–5.0 s | 15, 3.6–4.5 s | 15, 4.2–4.3 s | 15, 8.3 s |
| Write amp after writes, without WAL | 0.78× | 0.52× | 0.70× | 0.39× |
| Write amp after writes, with WAL | 1.80× | 1.54× | 1.72× | 1.40× |
| Full compaction to L6 wrote | 0.58× | 0.00× | 0.15× | 0.00× |
| **Write amp over the data's life, without WAL** | **1.36×** | **0.52×** | **0.85×** | **0.39×** |
| Space after writes (sst + blob) | 475 MB | 12 + 488 MB | 145 + 337 MB | 12 + 355 MB |
| **Space after full compaction** | **389 MB** | **500 MB** | **464 MB** | **367 MB** |
| Cold `Get`, recent, p50 / p99 | 266–296 µs / 1.0–1.1 ms | 254–283 µs / 1.2–1.4 ms | 304–371 µs / 1.3–1.4 ms | 272 µs / 2.2 ms |
| Cold `Get`, old, p50 / p99 | 372–396 µs / 1.8–2.0 ms | 493–576 µs / 1.9–2.0 ms | 485–599 µs / 2.0–2.1 ms | 515 µs / 2.9 ms |
| **Cold `Get` after full compaction, p50 / p99** | **479 µs / 1.6 ms** | **742 µs / 2.9 ms** | **682 µs / 3.3 ms** | **566 µs / 2.2 ms** |
| Warm `Get`, hot, p50 / p99 | 1.8–3.2 / 3.7–7.6 µs | 2.1 / 4.8–5.6 µs | 2.1 / 5.0–5.3 µs | 2.1 / 5.3 µs |
| Warm `Get`, recent, p50 / p99 | 7.4–7.6 / 20–27 µs | 7.6–9.7 / 27–28 µs | 8.6–8.8 / 32 µs | 14 / 54 µs |
| Warm `Get`, old, p50 / p99 | 13.7–14.6 / 36–61 µs | 8.9 / 25–29 µs | 19.6–20.2 / 43–50 µs | 19 / 69 µs |
| Warm `Get` after full compaction | 22 / 80 µs | 15 / 98 µs | 23 / 72 µs | 22 / 72 µs |
| Warm scan | 337–548 MB/s | 520–554 MB/s | 444–450 MB/s | 210 MB/s |

What the table shows:

- **Throughput is the same within noise,** except `onz`, whose zstd flushes stall longer. Separation makes flushes write blob files instead of sstables, and the flush still bounds the write path.
- **`on` writes the least.** Its compactions rewrite only keys and blob handles: 12 MB of sstables for 1 GB of Events. Without separation, the 306 MB left in L0 is still to be rewritten, and the full compaction wrote 0.58×.
- **The 2 KB threshold is the worst of both.** It separates 65% of Events, so it gets most of the read cost, but it saves less on writes.
- **A cold `Get` of a compacted Event reads one more block.** p50 rises by 25–55% and p99 by up to 80%. Recent Events, still in L0 without separation, cost the same either way.
- **Warm old `Get`s are faster with `on`.** Probably because its keys-only sstables fit the 8 MB block cache, and a MinLZ blob block decodes faster than a zstd one. With zstd blobs (`onz`) the advantage goes.
- **Space:** `on` keeps every value MinLZ (2.08× on 1,015 MB of values). `off` ends zstd in L6. `onz` is 6% smaller than `off`.

### A deeper store

With `PEBBLE_VALSEP_LBASE_MB=4`, the same 1 GB gets levels L3 to L6, as a store about 16 times bigger would with the real 64 MB.

| | off | on |
|---|---|---|
| Write throughput at saturation | 44.0k Events/s | 47.2k |
| Write amp after writes, without WAL | 0.78× | 0.55× |
| Full compaction to L6 wrote | 0.97× | 0.49× |
| Write amp over the data's life, without WAL | 1.75× | 1.04× |
| Space after full compaction | 387 MB | 385 MB |

- **The saving stays about the same:** 0.7× of the data written here, 0.8× in the shallower store. Without separation more is written, but so it is with separation, which now rewrites blob values.
- **Blob values get rewritten.** A compaction whose inputs reference more than `MaxBlobReferenceDepth` blob files writes the values into new blob files (`value_separation.go:93-98`). The full compaction rewrote 486 MB of them, compressed for L6 (zstd), so the space penalty went. A real store would pay that rewrite as it grows.

## Shared-storage tiering (`CreateOnShared`)

`TestTiering` opens the store with a shared tier on a local directory (`remote.NewLocalFS`), as config's `local` tier kind does, writes 256 MB, then compacts everything.

**With a tier at `Level` > 0** (`CreateOnSharedLower`, L5 and L6 shared):

- **Flushes write blob files locally.** 66 were created, all in the local directory.
- **Compactions into L5 and L6 put the values back.** After writes and compactions, no blob file was live and the shared directory held 21 sstables and no blob files.
- **Write amp is the same:** 1.05× with separation against 1.07× without. Separation only adds a blob file write at flush time.
- **A reopen reads all 118,498 Events back intact.**

**With a tier at `Level` 0** (`CreateOnSharedAll`, every level shared): no blob file was created at all.

**Pebble logged nothing but info lines**, such as "remote storage configured; creatorID = 1". `Options.Validate` (`options.go:2373`) doesn't check the combination: it checks `FormatMajorVersion` for `CreateOnShared` (`options.go:2398`), and `MinimumSize` and `MaxBlobReferenceDepth` for the policy (`options.go:2410-2417`).

The source lines that decide it, in `github.com/cockroachdb/pebble/v2@v2.1.7`:

- **Compactions:** `compaction.go:607-617`. When `remote.ShouldCreateShared(CreateOnShared, outputLevel)` is true, `c.getValueSeparation = neverSeparateValues`. `compact.NeverSeparateValues.Add` (`internal/compact/run.go:485-493`) fetches each value, from its blob file if need be, and writes it into the sstable.
- **Flushes:** `compaction.go:865-873`, the same check for the flush's output level, L0. Under `CreateOnSharedAll`, L0 is shared, so nothing is ever separated.
- **Which levels are shared:** `objstorage/remote/storage.go:55-66`. `CreateOnSharedLower` means `level >= SharedLevelsStart`, which is 5 (`storage.go:36`).
- **Copy compactions:** `compaction.go:671-688`, `maybeSwitchToMoveOrCopy`. A local table with blob references can't be moved or copied to shared storage, "as we currently lack a mechanism to propagate blob references along with the sstable". It is rewritten.
- **Blob file rewrites:** `blob_rewrite.go:62-65` sets `PreferSharedStorage: false`, with "TODO(jackson): Enable shared storage for blob files."

So with a tier, separation applies only to L0–L4. A store under about 700 MB on disk has no levels between L0 and L5 (`compaction_picker.go:800`, `initLevelMaxBytes`), so separation does nothing for it.

## Other findings

- **Blob compression:** a blob file takes the value-block compression of the level it is written for (`options.go:2481-2493`, `sstable/block/compressor.go:70-80`). `DBCompressionBalanced` gives L0–L4 MinLZ, L5 "Fast" and L6 zstd (`options.go:1320-1328`).
  - L5's "Fast" profile isn't enough for zstd blobs. Its adaptive compressor keeps MinLZ unless zstd saves a further 30% (`sstable/block/compression.go:71-78`), and Event JSON in 4 KB blocks doesn't. `onz` uses the Balanced profile for L0–L4.
- **Flushes start blob files; compactions mostly carry references on.** A compaction writes new blob files only when its inputs reference none (data written before separation was on) or more than `MaxBlobReferenceDepth` (`value_separation.go:78-101`). Events are never deleted, so with `TargetGarbageRatio` 1.0, blob files are never rewritten to reclaim space.
- **Pebble's blob metrics:** besides the double count above, `BlobFiles.Local` counts are wrong after a reopen. They read 0, or underflow to 2^64 − 162, for blob files that are all local.

## If it is turned on later

- **When:** if disk writes become the limit, for example under long saturation or in a much larger store. Or once Pebble supports shared blob files, if tiering is in use.
- **Settings:** `MinimumSize` 512, which separates every Event. `MaxBlobReferenceDepth` 10. `RewriteMinimumAge` 5 min.
- **Garbage:** set `TargetGarbageRatio` below 1, for example 0.2. Log batches live in the same store and are deleted once processed (`DeleteRange` in `storage/pebble_logs.go`). A flush can put a Log batch and Events in one blob file. The Events then keep that file alive, and the Log batch's value stays as garbage unless the file is rewritten. This was not measured.
- **Compression:** decide between MinLZ blobs (28% more space) and zstd at L0–L4 (slower flushes and warm reads).
- **Format:** stores are already at `FormatValueSeparation`, so turning it on needs no format change. Existing data is separated as it is compacted.

## Not measured

- **A real ingest rate.** Every run wrote at saturation. At 5k Events/s (11 MB/s) compactions should keep up easily, and separation would save about 10 MB/s of disk writes.
- **Garbage from deleted Log batches**, above.
- **A real bucket.** Tiering was tested only on a local directory. The source shows a bucket behaves the same way.
