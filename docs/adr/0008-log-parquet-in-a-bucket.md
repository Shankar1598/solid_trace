# Log Parquet and the Event archive in a bucket, read by DuckDB through httpfs

An install can keep its Log files and its Event archive in one S3 or GCS bucket (`object_store` in `event_store.yml`); without one, everything stays on local disk as before. Each Log file is uploaded as soon as it is published, and its local copy is kept for `local_cache_hours` (24 by default, 0 allowed). DuckDB reads a remote file in place through its httpfs extension, by its `s3://` URL, in the same `read_parquet([...])` list as local files. The manifest ([ADR 0007](0007-log-files-listed-in-a-manifest.md)) records whether each file is local, remote or both, so reads never list the bucket. We chose this because bucket storage costs far less than VM disk for data read rarely. And the [probe](../../.scratch/logs-v2/research/object-store-probe.md) showed httpfs loads into the static duckdb-go binary and is fast enough: a cold 7-day count-by-bin over 168 remote hour files took about 2 s with 30 ms added to each request. Warm, it sent no requests.

Both clouds are reached through the S3 API with aws-sdk-go-v2, GCS through its S3 interop endpoint with an HMAC key. So there is one adapter (`storage.ObjectStore`) and one kind of DuckDB secret (`TYPE s3`) for AWS, GCS, MinIO and R2, and the key is the same for EventStore's uploads and DuckDB's reads.

## Considered options

- **Go downloads remote files into the local cache before a read** (the spec's fallback): it needs no extension, but every first read of an old hour waits for whole files, and the cache needs its own space and eviction. Not needed, because httpfs loads.
- **GCS's own client (`cloud.google.com/go/storage`) and `gs://` URLs:** a second code path and a second secret type, for nothing DuckDB or EventStore needs.
- **Reusing Pebble's `s3_storage.go` and `gcs_storage.go`:** they serve Pebble's `remote.Storage` interface, buffer whole objects in memory and take credentials from the default chains.
- **DuckLake:** rejected in [ADR 0005](0005-logs-per-project-hot-tables-promote-at-ingest.md) for reading keys through `coalesce`. The manifest now records what its catalog would, and it was not probed again.
- **Daily compaction into one file per day:** not built. At today's file sizes a cold 7-day chart costs about 3 requests per hour file. For busy Projects the cost is one request per row group per column read, which only compaction with much larger row groups would cut. The bin cache serves repeat runs. Revisit it if long-range charts are slow on their first run.

## Consequences

- **httpfs is a separate file tied to DuckDB's version.**
  - The Docker image downloads it for its architecture into `duckdb_extension_directory` (`/usr/local/lib/solidtrace/duckdb_extensions`).
  - EventStore turns DuckDB's extension autoinstall and autoload off and loads it from there, so DuckDB never downloads code while running.
  - DuckDB checks the file's signature when it loads it.
  - Upgrading duckdb-go means changing `DUCKDB_VERSION` in the Dockerfile too.
- **One secret serves every connection.** It is temporary, held in memory, scoped to the bucket and created once at startup. Every pooled connection uses it, and so does SQL mode ([ADR 0003](0003-unrestricted-sql-queries-behind-an-instance-setting.md)). The key comes from `SOLIDTRACE_OBJECT_STORE_KEY_ID` and `SOLIDTRACE_OBJECT_STORE_SECRET`, never from the config file.
- **DuckDB's caches are on while a bucket is configured:** Parquet metadata, HTTP metadata and the external file cache. They stay within `memory_limit`. The HTTP metadata cache is never invalidated, so an object's key must never be written again with different bytes.
- **GCS's interop API has quirks the adapter allows for:**
  - checksums are sent only where S3 requires them;
  - GCS's front end rewrites `Accept-Encoding` before GCS checks a request's signature, so the adapter leaves that header out of the signature (it is still sent);
  - GCS rejects the `x-id` query parameter aws-sdk-go-v2 adds to PUT and DELETE, so the adapter drops it;
  - requests use path-style URLs whenever an endpoint is set;
  - listing uses ListObjects version 1;
  - a delete of a missing object answers 404 NoSuchKey, which counts as done; any other 404, such as NoSuchBucket, is an error, because nothing was deleted;
  - there is no multi-object delete, so objects are deleted one at a time.
- **A file is uploaded in a single PUT**, streamed from disk, so a file can be at most 5 GB.
- **What's left, for later units:** uploads, eviction after `local_cache_hours`, the disk limit's preference for evicting uploaded copies, deleting bucket objects through tombstones, and the Event archive's upload.

Source: [Logs v2 spec, Phase 5](../../.scratch/logs-v2/spec.md).
