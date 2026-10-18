# 12: Read-only SQL can fetch web addresses on bucket installs

**Status:** resolved
**Track:** Go
**Blocked by:** none

## What happens

Ticket 04 made SQL queries read-only: any SQL that would change data, settings or files is refused before it reaches DuckDB. Reading isn't limited, and that's by design (ADR 0003). One kind of read can reach beyond SolidTrace, though:

- On an install with an `object_store` bucket, EventStore loads DuckDB's httpfs extension so it can read the bucket (`event_store/storage/duckdb_bucket.go`, `LOAD httpfs`).
- httpfs reads `http://` and `https://` URLs as well as `s3://` ones. So a plain SELECT that reads a URL with a table function passes the read-only check. The server then fetches that URL and returns the answer: any address the server can reach, internal services and cloud metadata included.
- Without a bucket, httpfs isn't loaded and extension autoload is off (ticket 04), so those URLs fail.

Found by ticket 04's review (04-review-1). The build fixed it by warning rather than blocking.

## Why it matters more with the MCP

In the explorer, a member writes the SQL. Through `run_sql`, an agent writes it, and the agent has read Log text written by whoever sends Logs. A Log can carry instructions that steer the agent into fetching an internal address. One query can't put table data into a URL, because DuckDB refuses subqueries in table-function arguments. A steered agent can still do it in two calls: read the data, then fetch a URL it built from what it read.

## What protects an install today

- The SQL queries setting is off by default.
- The setting's description (`console/app/models/instance_setting.rb`), ADR 0003's amendment and `docs/logs/sql.md` say SQL can fetch web addresses when a bucket is configured. Ticket 10's `docs/mcp.md` will say it too.
- Advice until this is decided: keep SQL off on bucket installs unless you trust everyone in every Organization, and every agent they connect.

## To decide

1. **Keep the warning.** This is the state now.
2. **Turn off DuckDB's HTTP file system at open, but keep S3.** DuckDB has a `disabled_filesystems` setting. User SQL can't set it back, because SET statements are already refused. This is the smallest structural fix, if it works. Not yet checked: whether `s3://` and the GCS bucket still read with `HTTPFileSystem` disabled. That needs a probe of under a minute and the MinIO bucket tests, which need the `solidtrace-minio` container.
3. **Leave `run_sql` out of the MCP on bucket installs,** or put it behind a setting of its own. This narrows the MCP risk only; the explorer is unchanged.
4. **Turn off `enable_external_access` and allow only certain paths.** This is close to the sandbox ADR 0003 rules out, and it may block the bucket.

Option 2 is the one to check first. Whichever is chosen, amend ADR 0003 and update the setting's description to match.

## Comments

### 2026-10-18 resolved

The user asked for a fix. Option 2 alone wasn't enough: a probe on DuckDB 1.5.6, with MinIO and a local listener standing in for an internal service, found two more ways in. Its notes are in [probe-12](../research/probe-12-sql-web-addresses.md).

- **The query string.** An S3 URL takes its settings from its query string. `s3://other/x?s3_endpoint=127.0.0.1:PORT&s3_use_ssl=false` sent the request, and returned the answer, with `HTTPFileSystem` off. It also worked inside the bucket's own secret, and through `s3a`, `s3n`, `gcs`, `gs` and `r2`.
- **The bucket name as a host.** A virtual-hosted URL makes the bucket name the host. `s3://127.0.0.1:PORT#/x` became `https://127.0.0.1:PORT#.s3.amazonaws.com/x`, which connects to 127.0.0.1:PORT, so it could still reach internal HTTPS services and scan ports.

`UseBucket` now sets three global settings after making the secret (`event_store/storage/duckdb_bucket.go`):

- `disabled_filesystems = 'HTTPFileSystem,HuggingFaceFileSystem'`: `http://`, `https://` and `hf://` are refused. DuckDB can't turn them on again, even with `SET`.
- `s3_url_compatibility_mode = true`: an S3 URL's query string is part of its key. S3 globs are off too, which costs nothing, since EventStore names each file.
- `s3_url_style = 'path'`: an S3 URL outside the bucket's secret goes to S3's own host, with the bucket in the path.

**AWS buckets:** a secret with no style follows `s3_url_style`, so an AWS bucket's secret now names `URL_STYLE 'vhost'`, AWS's own style. Endpoint secrets (MinIO, GCS, R2) keep `path`.

**What's left:** SQL now reaches the bucket's endpoint and S3's and Google Cloud Storage's public hosts, and nothing else. There it can read public buckets, or carry what it read in a request's path to someone's bucket. Those are public hosts, and an agent can send what it read anywhere with its own tools.

**Tests:**
- `TestWithABucketSQLReachesNoOtherHost` tries each way in: `http`, `https`, the query string inside and outside the secret, and `#` hosts through `s3`, `s3a` and `r2`. Each must be refused without one connection to the listener, and `hf://` must be refused. It stays offline: it points S3's default endpoint at a closed port.
- `TestAnAWSBucketsSecretKeepsVirtualHostedURLs` checks both secret styles.

Both failed before the fix, every case of the first among them. The Go suite passes with the MinIO bucket tests (10 packages), and so does Rails (575 runs).

**Docs:** ADR 0003 has a new amendment, and its first amendment and Consequences now point to it. The setting's description (with its test), `docs/logs/sql.md`, `docs/mcp.md` and `docs/logs/object-store.md` say SQL reaches no web address but S3's and GCS's.
