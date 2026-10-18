# Unrestricted SQL queries behind an off-by-default instance setting

SQL queries run raw DuckDB SQL exactly as written, with no validation, sandbox or Project scoping, but only on installs whose operator turns on one instance-level setting (off by default). Research showed that making untrusted SQL safe on DuckDB needs structural isolation (a separate instance or process seeing one Project's data), because instance-wide settings, statements that run during prepare, `main.<table>` and `query()` all escaped lighter checks; building that was judged not worth it for v1. Instead, the operator opts in knowing that any member can then read every organization's Logs and Events and server files, write files, drop tables and starve ingest. Don't add partial checks (AST allow-lists, scoping CTEs) as if they made it safe: they were shown not to.

## Amended: SQL queries are read-only (2026-10-17)

Before an SQL query runs, EventStore has DuckDB parse it with `json_serialize_sql`, the SQL bound as a parameter so that none of it runs, and refuses it unless it is exactly one SELECT that calls none of the functions that change state from a SELECT: `query`, `query_table`, `json_execute_serialized_sql`, the logging, profiling, PEG parser and checkpoint functions, `write_log`, `nextval` and `setseed`. DuckDB's extension autoinstall and autoload are off on every install, so a query can't load an extension whose functions write. A refusal says "SQL queries are read-only: … isn't allowed".

This came with the MCP server, whose `run_sql` tool lets an agent run SQL that Log text written by senders may have steered. It is a narrow check, not the sandbox this record rules out: SQL can't change data, settings or files any more, but it can still read every Organization's Logs and Events, files on the server and the bucket, and starve ingest with slow queries. On an install with a bucket it could also send requests from the server to any web address the server can reach, such as internal services or cloud metadata, and read the answers, until the next amendment. So the setting stays off by default, and it still makes sense only where the operator trusts every member of every Organization. The check follows DuckDB 1.5.6's functions; a DuckDB upgrade should look for new ones that change state.

## Amended: SQL reaches no web address but S3's (2026-10-18)

With a bucket, EventStore loads httpfs, and httpfs read more than the bucket. It read `http://`, `https://` and `hf://` URLs. It took an S3 URL's endpoint from its query string (`s3://x/y?s3_endpoint=host`), even inside the bucket's secret. And it put an S3 URL's bucket in the host, so `s3://host:port#/x` reached `host:port`. So SQL could send requests from the server to any address it could reach, internal services and cloud metadata among them, and read the answers. Through `run_sql`, Log text could steer an agent into doing it.

`UseBucket` now keeps httpfs to S3 URLs, with three global settings:

- **The HTTP and Hugging Face file systems are turned off** (`disabled_filesystems`). DuckDB can't turn a file system on again once it's off, even with `SET`.
- **An S3 URL takes no settings from its query string** (`s3_url_compatibility_mode`). This also turns off globs in S3 URLs, which EventStore doesn't use: it lists its files.
- **An S3 URL outside the bucket's secret is path-style** (`s3_url_style`), so its bucket is a path on S3's host, never a host of its own. The bucket's secret names its own style, so an AWS bucket stays virtual-hosted.

SQL can't change the settings, since `SET` is refused. It now reaches only the bucket's endpoint and S3's and Google Cloud Storage's public hosts. There it can still read public buckets, or carry what it read in the path of a request to someone's bucket. Those hosts are public, and an agent can send what it read anywhere with its own tools. `TestWithABucketSQLReachesNoOtherHost` tries each way in. A DuckDB upgrade should look for new file systems in httpfs, and for new URL settings.

## Consequences

- Users query a stable `logs` view, not the storage tables, so storage layout changes don't break their SQL.
- Since [ADR 0005](0005-logs-per-project-hot-tables-promote-at-ingest.md) the `logs` view shows only the explorer's Project and time range. That is a convenience, not Project scoping: the read-only check (above) is the only check, and the SQL can read the raw tables.
- If SQL must later be offered on installs with untrusted members, the fix is the structural sandbox, and existing cross-Project SQL will break.
- On an install with an `object_store` bucket ([ADR 0008](0008-log-parquet-in-a-bucket.md)), SQL mode can read the bucket through the instance's DuckDB secret, which every connection shares. Any `s3://<bucket>/…` URL works, including other Projects' Log files and the Event archive. Since SQL queries are read-only, it can't write there: only `COPY` writes through httpfs. The httpfs extension that reads the bucket reads only S3 URLs (the next amendment), so SQL reaches the bucket's endpoint and S3's and Google Cloud Storage's public hosts, and no other web address. Without a bucket httpfs isn't loaded, and no remote URL reads.

Source: [What query language(s) do users write?](../../.scratch/logs/issues/11-query-language.md).
