# Object store probe (Phase 5, step 2)

2026-10-11, unit B4a. Can the static duckdb-go binary read Log files from a bucket through httpfs, and how fast are long-range charts over them? Run on the 4-CPU dev box against MinIO in Docker (`solidtrace-minio`, see `~/.cache/solid-trace/logs-v2-notes/setup.md`), with duckdb-go v2.10506.0 (DuckDB v1.5.6).

The probe is `event_store/bench/objectstore/` (`probe_test.go`, `proxy_test.go`). Each step is one test that skips unless `OBJECTSTORE_PROBE=1`; each run takes under a minute. The package comment has the command.

## Answer

- **httpfs loads.** No fallback is needed: Log files can be read straight from the bucket.
- **One `CREATE SECRET` serves every pooled connection.** That includes connections opened later.
- **Local and `s3://` files mix in one `read_parquet([...], union_by_name = true)`.**
- **Charts over the bucket are fast enough at today's file sizes.** A cold 7-day count-by-bin took 1.9 s with 30 ms added to every request, and 0.3 s warm. **Daily compaction is not needed now** (see the recommendation at the end).
- **Turn on all three caches.** `parquet_metadata_cache`, `enable_http_metadata_cache` and `enable_external_file_cache` together cut a cold run's requests by 43% and make a warm run send none.

## 1. Loading httpfs into the static binary

- **Download:** `https://extensions.duckdb.org/v1.5.6/linux_amd64/httpfs.duckdb_extension.gz`. Gunzipped, it is 22 MB.
- **Where it goes:** `<extension_directory>/v1.5.6/linux_amd64/httpfs.duckdb_extension`.
- **Settings:** the database is opened with `autoinstall_known_extensions=false`, `autoload_known_extensions=false` and `extension_directory` set, all three in the DSN.
- **Result:** `LOAD httpfs` succeeds, with `allow_unsigned_extensions` left false, so DuckDB checks the file's signature. duckdb-go's prebuilt static library links with `-rdynamic` (`duckdb-go-bindings/lib/linux-amd64 prebuilt.go`), so the extension finds DuckDB's symbols in the Go binary. `duckdb_extensions()` reports httpfs as loaded from that path, version `4bc690d`.
- **Before LOAD:** reading an `s3://` file fails with `Missing Extension Error: File s3://… requires the extension httpfs to be loaded`. Nothing is fetched from the network.
- **One LOAD is enough:** loading on one pooled connection loads httpfs for every connection of the database.

## 2. Secrets and pooled connections

- **Every connection sees the secret.** Three connections were held at once, so each was a different one. `CREATE SECRET` was run on the first. All three read the bucket, and so did a fourth connection opened afterwards.
- **It lives in memory only.** `duckdb_secrets()` on each connection lists it with `persistent = false` and `storage = memory`. A temporary secret belongs to the database instance, not to a connection, so nothing is written to disk.
- **Without a secret:** httpfs sends the request with no credentials to AWS's default endpoint. The read then fails, with 404 or 403 from AWS.
- **SCOPE:** with `SCOPE 's3://<bucket>/'`, a path in another bucket gets no credentials ("No credentials are provided"), so a SQL query can't use the key on another bucket.
- **The secret used:**

  ```sql
  CREATE OR REPLACE SECRET solidtrace_bucket (TYPE s3, KEY_ID '…', SECRET '…',
    ENDPOINT '127.0.0.1:9000', URL_STYLE 'path', USE_SSL false, REGION 'us-east-1',
    SCOPE 's3://solidtrace-dev/')
  ```

## 3. Mixed local and bucket files

One day's 24 hour files were read in one `read_parquet([...], union_by_name = true, hive_partitioning = false)` list: the even hours from local paths, the odd hours as `s3://` URLs. `count(*)`, `count(DISTINCT id)`, `min(time)` and `max(time)` matched the all-local read (384,000 Logs) exactly. It took 75 ms.

## 4. Chart timings

### Data

- **Generated through the export path** (`LogStore.AppendLogBatches` then `ExportLogs`), from the logs bench's realistic Logs (`bench/logslayouts`). That is 16,000 Logs an hour for 24 hours.
- **Seven days:** the day was copied to six more with each Log's time moved on, written with the export's options (`ORDER BY time, id`, zstd, `ROW_GROUP_SIZE 30720`).
- **Totals:** 168 files, 281 MB, 1.7 MB each. Each file has one row group, 117 leaf columns and an 18 KB footer.

### Queries

- **The charts:** a count by 5-minute bins over 24 hours (288 bins), and by 1-hour bins over 7 days (168 bins).
- **Their shape follows `LogStore`'s reads:** one `read_parquet([...], union_by_name = true)` per day, filtered on `time`, joined by `UNION ALL BY NAME`.
- **Cold and warm:** cold is the first run on a newly opened database; warm is the same query again.

### How requests were measured

- **The proxy:** every bucket read went through a small reverse proxy in front of MinIO, which counts requests by kind. SigV4 still holds, because the Host header is kept.
- **Simulated latency:** the proxy can wait before each request (`OBJECTSTORE_PROBE_LATENCY`), and again before a connection's first request (`OBJECTSTORE_PROBE_CONNECT`). This stands in for a far bucket.
- **The latency used:** 30 ms per request, plus 5 ms per new connection, as an estimate of GCS from a VM in the same region. GCS's time to first byte for a small ranged read is usually tens of milliseconds, and the network round trip inside a region is about 1 ms. A real GCS run (spec, Verification) should replace these figures.

### Results

"Caches" means `parquet_metadata_cache`, `enable_http_metadata_cache` and `enable_external_file_cache` all on, or all off. DuckDB's defaults on 1.5.6 are off, off and on. Threads default to 4 on this box.

| Files | Latency | Threads | Caches | 24 h cold | 24 h warm | 7 d cold | 7 d warm |
|---|---|---|---|---|---|---|---|
| local | – | 4 | off | 76 ms | 28 ms | 294 ms | 182 ms |
| local | – | 4 | on | 26 ms | 20 ms | 163 ms | 144 ms |
| MinIO | 0 | 4 | off | 165 ms | 148 ms | 902 ms | 847 ms |
| MinIO | 0 | 4 | on | 111 ms | 29 ms | 563 ms | 200 ms |
| MinIO | 30 ms | 4 | off | 1.50 s | 1.51 s | 9.17 s | 9.08 s |
| MinIO | 30 ms | 4 | on | 869 ms | 23 ms | 5.24 s | 172 ms |
| MinIO | 30 ms | 16 | off | 498 ms | 496 ms | 3.40 s | 3.31 s |
| MinIO | 30 ms | 16 | on | 346–366 ms | 27–38 ms | 1.85–1.90 s | 167–322 ms |

The MinIO rows with 30 ms latency also add 5 ms per new connection. The rows with 16 threads ran with `httpfs_connection_caching` both off and on; ranges show both runs.

### Requests per chart

The requests below are the same at any latency.

| Caches | 24 h (24 files) | 7 d (168 files) |
|---|---|---|
| off, cold or warm | 144: 24 HEAD, 120 ranged GET (2.7 MB) | 1,008: 168 HEAD, 840 GET (19.3 MB) |
| on, cold | 96: 24 HEAD, 72 GET (1.9 MB) | 576: 144 HEAD, 432 GET (11.6 MB) |
| on, warm | 0 | 0 |

- **Without the caches**, every run reads each file twice, 6 requests a file: a HEAD and a footer read while binding (`union_by_name` reads every footer), then the footer and data again while scanning.
- **With them**, a cold run makes about 3.4 requests a file, and a warm run reads only memory.
- **New connections:** with `httpfs_connection_caching` off (the default), httpfs opened about one new connection for every 2 requests: 288 for the cold 7-day chart. With it on, the 4-thread run opened 4, and the 16-thread run opened 16. Against GCS each new connection costs a TCP and TLS handshake. With the 5 ms simulated cost it saved about 8% (5.24 s down to 4.85 s at 4 threads).

### Row groups decide the request count

- **The test:** the 24 files were rewritten with 8 row groups each (192 in all), and a cold 24-hour chart with the caches on was run over them.
- **Result:** 264 requests (24 HEAD, 240 GET), against 96 with one row group per file.
- **What it costs:** each row group costs one ranged GET for each column the query reads, plus about 3 requests per file. A count where `level = 'error'` made 384 GETs, two per row group.
- **At the target rate:** one Project at 3,200 Logs/s has 11.5 million Logs an hour, which is 375 row groups of 30,720. A cold 7-day count-by-bin over such hours would make about 168 × 378 ≈ 63,500 requests. At 30 ms each and 16 in flight, that is about 2 minutes. It would also read several GB of `time` column. The probe's data carried 4.3 B a Log for this chart, about 8 GB for 1.9 billion Logs.

### Memory

- **After the warm runs** (168 files, caches on), `duckdb_memory()` showed `OBJECT_CACHE` at 29 MB (parsed footers, about 170 KB a file) and `EXTERNAL_FILE_CACHE` at 15 MB.
- **Both stay within `memory_limit`.** With `memory_limit = 30MB` they shrank to 1.1 MB and 0.6 MB, and the warm 7-day run read the data again (840 GETs). It sent no HEADs, because the HTTP metadata cache still held them. So the caches can't take memory past the limit; when memory is short, warm runs just read the files again.

### Settings on 1.5.6

| Setting | Default | Scope | Notes |
|---|---|---|---|
| `parquet_metadata_cache` | false | parquet | Can't go in the DSN, because the Parquet reader registers it after the database opens. Use `SET GLOBAL`. |
| `enable_http_metadata_cache` | false | core | Caches HEAD answers (size, last modified) for the life of the process. |
| `enable_external_file_cache` | true | core | Caches ranges read from remote files in the buffer pool. |
| `validate_external_file_cache` | `VALIDATE_ALL` | core | Also `VALIDATE_REMOTE` and `NO_VALIDATION`. With the HTTP metadata cache on, validation sends no requests. |
| `httpfs_connection_caching` | false | httpfs | Exists only after `LOAD httpfs`. |
| `http_keep_alive` | true | httpfs | |
| `http_retries`, `http_retry_wait_ms`, `http_retry_backoff`, `http_timeout` | 3, 100, 4, 30 | httpfs | |

## GCS, estimated

MinIO on localhost hides latency, so the 30 ms rows are the estimate for GCS. Requests per chart don't depend on latency; time grows with requests ÷ threads × time to first byte.

- **24 hours, cold:** 96 requests, about 0.35 s at 16 threads or 0.9 s at 4.
- **7 days, cold:** 576 requests, about 1.9 s at 16 threads or 5.2 s at 4.
- **Warm:** no requests. In production the most recent `local_cache_hours` (24 by default) are read from local disk anyway.
- **Cost:** GCS charges $0.004 per 10,000 Class B operations (GET, HEAD), so a cold 7-day chart costs about $0.0002. LIST is never used, because reads name every file from the manifest.

## Recommendations

**Turn on all three caches when a bucket is configured.** That is `parquet_metadata_cache`, `enable_http_metadata_cache` and `enable_external_file_cache`.
- They cut a cold run's requests from 6 to about 3.4 a file, and a warm run's to none.
- `parquet_metadata_cache` also speeds up local reads: the 7-day chart went from 294 ms to 163 ms cold. It could be turned on without a bucket too, but that is left for later, so local mode stays exactly as it is.
- The HTTP metadata cache is never invalidated within a process. So an object key must never be written again with different bytes while EventStore runs. Today a name can return only after its tombstone is swept, for example late Logs exported into an hour whose files Retention deleted. B4b should make keys unique for good, for example by never reusing a name the manifest has seen for that hour, or by adding a unique suffix to the key.

**Raise DuckDB's threads for remote reads, in B4b.** httpfs blocks a thread for each request, so threads limit how many requests are in flight. 16 threads cut the cold 7-day chart from 5.2 s to 1.9 s on this 4-CPU box. That setting is global and changes local queries too, so B4b, which brings remote reads, should measure it and decide. Options are 4 × CPUs while a bucket is configured, or a config key.

**`httpfs_connection_caching`:** worth trying against real GCS in the manual run. It saved most handshakes here, but it is off by default upstream, so B4a leaves it off.

**Daily compaction (step 7): not now.**
- At the probe's file sizes a cold 7-day chart takes about 2 s, and a warm one 0.2–0.3 s.
- **For a quiet Project** (one row group an hour), merging each day into one file saves the ~3 requests each hour file costs. That would cut the cold 7-day chart from 576 requests to about 110, or from about 1.9 s to about 0.5 s. That is a gain, but 2 s for a chart's first cold run is acceptable, and B5 serves repeat runs.
- **For a busy Project**, one request per row group per column dominates, and the file overhead is under 1% of it. Compaction would help only if it also wrote much larger row groups (around 1M rows). The export's 30,720 rows were chosen for local searches (ticket 09).
- Even then, a 7-day scan of 1.9 billion Logs reads gigabytes of `time` column, which compaction doesn't shrink.
- The bin cache (B5) is the better lever. Closed bins are computed once per Query and reused, so dashboards and Monitors re-read only the live span.
- Revisit compaction if the real GCS run, or a busy Project after B5, shows long-range charts slow on their first run. It would merge a closed day into one file with row groups of about 1M rows, swapped in the manifest in one transaction.

**Fallback (Go downloads remote files before reading): not needed**, because httpfs loads.

## Cleaning up

`TestProbe9Clean` deletes the generated files (`~/.cache/solid-trace/probe-objectstore`) and the bucket's `probe-objectstore/` objects. It was run after the probe.
