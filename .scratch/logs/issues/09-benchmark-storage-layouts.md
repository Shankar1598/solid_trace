# 09: Benchmark the shortlisted storage layouts at target scale

Type: task
Status: open
Blocked by: 03, 07
Map: [SolidTrace Logs](../map.md)

## Question

AFK task. Build a throwaway benchmark of the layouts shortlisted by the DuckDB storage research, using the Log shape from the domain model, on a **DuckDB 2.0 preview or RC build driven from Go**. First establish whether duckdb-go has a build for 2.0; if it doesn't, record that as a finding, then run on the closest available engine and say which. Measure sustained write rate, query latency on fixed and arbitrary fields over a day of data (100–170 GB uncompressed), on-disk size, and memory, at the map's target scale (average 1.2–1.9 MB/s uncompressed) and at 5× that as a burst probe. Record the numbers and the machine they ran on.

VARIANT is the primary candidate (DuckDB 2.0 names "real-time log ingestion" as its design case, and 2.0 reads and writes shredded VARIANT in Parquet); JSON-as-text and typed maps are the fallbacks. For VARIANT, answer specifically:
1. Can Go write VARIANT natively on the 2.0 build (Appender, bound parameters), or only as `text::JSON::VARIANT`, and what does that cost in write rate?
2. Are small flush batches (about 1–6k logs per second at target scale) shredded, or only large row groups (30k+ rows on 1.5)? If not, how large or how late must batches be before arbitrary-field filters run fast?
3. Do filters on shredded fields prune data (in the hot table and in Parquet written by DuckDB), and do results match the JSON baseline row for row?

DuckDB 2.0's asynchronous I/O ([highlights §5](https://duckdb.org/2026/08/17/duckdb-20-highlights#5-asynchronous-io), [design post](https://duckdb.org/2026/07/31/asynchronous-io.html)) is the other engine feature to measure, because it decides how attractive an object-storage cold tier is:
4. Query the cold Parquet tier on local disk and on S3 (or an S3-compatible store), async on and off (`read_ahead_depth = 0`), for time-range and arbitrary-field queries. Does it confirm the published ~3.7× gain on S3 and ~3× gain on many small files?
5. How many rows per row group and row groups per file work best for both VARIANT shredding and async parallelism?
6. How much memory do read-ahead and the async thread pool (`async_threads`) use while ingest runs at target rate, and which settings keep a query from starving ingest?
7. Are Parquet writes (archive and compaction) and native `.duckdb` reads asynchronous on the build tested? The July post says no; the August highlights say both landed.

Input (2026-10-02, from [What is a Log?](07-log-domain-model.md)): the Log shape is seven fixed columns (`time`, `level`, `message`, `service`, `environment`, `trace_id`, `span_id`) plus an id, and one flat set of dotted Attributes whose values keep their own type (one Attribute may be a number in some Logs and a string in others). There is no received-time column.

## Comments

**2026-09-27, from [upgrade-pebble-duckdb 01](../../upgrade-pebble-duckdb/issues/01-upgrade-pebble-and-duckdb.md):** the "does duckdb-go have a 2.0 build?" check is done. It does not: the `v2.20000.0-N.preview` tags report `version()` = v1.5.4 (with 2.0 backports), not 2.0. To benchmark the real 2.0 alpha from Go, build with `-tags=duckdb_use_lib` against the alpha's `libduckdb.so` from https://duckdb.org/install/preview.html. In a scratch test, EventStore's storage and handler tests passed that way against v2.0.0-alpha43546. Once 2.0.0 ships, `duckdb-go/v2@v2.20000.0` should be the plain Go option. **Update:** EventStore itself now links the alpha this way. The benchmark can reuse `event_store/scripts/fetch-duckdb` and the `mise.toml` env, and `TestDuckDBEngineVersion` shows how to confirm the engine.
