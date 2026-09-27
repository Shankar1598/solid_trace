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
