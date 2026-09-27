# 03: How can DuckDB store schemaless logs at 10k logs/s?

Type: research
Status: resolved
Blocked by: none
Map: [SolidTrace Logs](../map.md)

## Question

What are the realistic ways to store and query schemaless logs with DuckDB from Go at around 10k logs/s sustained and 50–100 GB/day raw on one VM? Compare: JSON column, typed MAP columns with a field catalog, VARIANT/shredding (which DuckDB version has it, and how stable it is), Parquet files with a manifest, DuckLake, and keeping Pebble in the path. For each: write path (Appender, batch size), query speed on arbitrary fields, compression ratio, retention/deletion unit, S3/GCS tiering story, and the engine version this repo pins versus the current release. Also note DuckDB alternatives only if a hard blocker appears. Produce a shortlist of layouts worth benchmarking.

Starting points: `.scratch/logging-old/research/research-duckdb.md`, `explore-go-internals.md` (unverified).

## Answer

Full findings: `.scratch/logs/research/03-duckdb-log-storage.md` on branch `research/duckdb-log-storage` (commit `3389ebc`); read with `git show research/duckdb-log-storage:.scratch/logs/research/03-duckdb-log-storage.md`.

- **No hard blocker in DuckDB** for schemaless logs at this scale.
- **Versions:** the repo pins duckdb-go v2.5.4 (DuckDB 1.4.3). Current releases are 1.5.5 (duckdb-go v2.10505.0) and 1.4.5 LTS; 1.4 LTS reaches end of life on 2026-11-17. DuckDB 2.0.0 is scheduled for 2026-10-21 and is feature-frozen.
- **VARIANT is the obvious schemaless type but immature for us:**
  - It needs DuckDB 1.5 and database files at storage version v1.5.0 (the default is still v1.0.0).
  - It only shreds row groups of at least 30k rows.
  - Every 1.5.x patch fixed VARIANT bugs, including wrong filter results in 1.5.4.
  - duckdb-go cannot append, bind or scan VARIANT; the only path is `text::JSON::VARIANT` via the Query Appender. Full Go support is planned for the 2.0 line.
- **JSON** is stored as text and parsed on every query. **Typed MAPs** need one MAP per value type, a field catalog we build ourselves, and they get no per-key data skipping.
- **Retention and S3/GCS tiering:** the native `.duckdb` file is weak (partial space reclaim, read-only on S3). Parquet (with a manifest) and DuckLake retire whole files and can write to S3/GCS. DuckLake 1.0 needs DuckDB 1.5.2+, and a DuckDB-file catalog allows a single client.
- **Today's EventStore DuckDB** never touches object storage (tiering in the README is Pebble-only). The ingester flushes on a 1 s timer, and a failed write drops the batch.
- **Concurrency:** one read-write process for every option. Checkpoints stop blocking reads and writes only from 1.5.
- **The map's scale target contradicts itself:** 10k logs/s at 50–100 GB/day is only 58–116 bytes per log. Which figure binds changes WAL and checkpoint pressure by about 10×.
- **New candidate:** sealed per-window `.duckdb` files read with `read_duckdb` (1.5); filter pushdown through it is unverified.
- **Benchmark shortlist** (for the benchmark ticket), all on 1.5.5 with a 2.0 RC re-run where possible:
  1. hot-table attributes: JSON vs typed MAPs vs VARIANT
  2. cold Parquet: hourly vs daily files, attribute encodings, manifest vs glob, local vs S3
  3. DuckLake as the cold tier
  4. Pebble-first ingest, only if durability or raw-line fetch needs it

Open questions it hands to decisions: which scale figure binds; engine line (1.4, 1.5 now, or 2.0); whether VARIANT's churn is acceptable in v1; shared vs separate DuckDB file for logs; hot window length and cold-tier format; retention per project or global; S3/GCS in v1; keeping raw bytes; air-gapped installs (extensions download at runtime); isolating user queries from ingest.
