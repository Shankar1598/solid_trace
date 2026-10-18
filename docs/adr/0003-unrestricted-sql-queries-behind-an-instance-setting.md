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

## Amended: SQL reads only its own Project (2026-10-18)

Once sign-up works, a stranger who signs up could turn SQL on an install into a read of every Organization's Logs: the hot tables, the global `events` view, every Log and Event file by path or wildcard, the bucket and the catalogue. Refusing SQL that names another Organization couldn't work: tables are named by Project, the `events` view mixes every Project, and a wildcard names nothing. So the check now allows only what is known to be safe, from the same parse, and refuses the query unless every table reference is one of these:

- `logs` or `events`, unqualified: the temporary views RunSQL makes on the query's connection for its Project and time range. The temporary `events` view hides the global one, which it names with its database.
- a `WITH` part in scope, as DuckDB binds it: each part sees only the parts before it, and a recursive one itself. A `WITH` name must be a plain identifier, which DuckDB can't read as a file, and mustn't be a table or view in the database, the global `events` view included, so a slip in following scope can't reach the real one.
- a subquery, join, `VALUES` list, `PIVOT` or `UNPIVOT` over the above, or `DESCRIBE`, `SHOW` or `SUMMARIZE` of them.
- `range`, `generate_series` or `unnest`, the table functions that read nothing.

Everything else is refused, with "SQL queries can read only the logs and events views: … isn't allowed": qualified names, file paths and URLs in `FROM` (DuckDB parses them as table names), every other table function, table macros among them, `SHOW TABLES`, `information_schema` and `pg_catalog`, and the scalar functions that read settings or the catalogue (`current_setting`, `getvariable`, `pg_get_viewdef`, `pg_get_constraintdef`). The check sees only the user's SQL, not the views' definitions, so the views' own `read_parquet` keeps working.

This is a check, the kind of AST allow-list this record warned about, not the structural sandbox. It is acceptable now because the ways round lighter checks that the research found are already closed: `SET` and every statement but one SELECT are refused, nothing of the SQL runs during the parse, and `query()` and its kind are refused. With file paths and URLs refused, SQL also reaches no web address at all, bucket or not. A DuckDB upgrade should look for new kinds of table reference, new table functions and table macros, new scalar functions that read settings, files or the catalogue, and new replacement scans. Saved SQL that read raw tables, files or other Projects stops working, Dashboard charts included.

## Amended: SQL queries are on by default (2026-10-18)

With SQL reading only its own Project (the amendment above) and stopped after 10 seconds, whatever it asks for, so that a slow query holds DuckDB's threads from ingest for no longer, the setting is on by default. The column default changed, and EventStore's default for a missing row with it ([ADR 0001](0001-eventstore-writes-console-tables.md)). An install whose operator set the setting keeps what was set. The operator can still turn SQL off. This record's title and opening describe the setting as it began.

## Consequences

- Users query a stable `logs` view, not the storage tables, so storage layout changes don't break their SQL.
- Since [ADR 0005](0005-logs-per-project-hot-tables-promote-at-ingest.md) the `logs` view shows only the explorer's Project and time range. Since the amendment above it is a limit too: with the `events` view, it is all SQL can read.
- If the check ever proves not enough, the fix is the structural sandbox.
- On an install with an `object_store` bucket ([ADR 0008](0008-log-parquet-in-a-bucket.md)), the views read the bucket through the instance's DuckDB secret, which every connection shares. SQL itself can't name an `s3://` URL since the amendment on reading only its own Project, so it reads none of the bucket but its Project's views. httpfs is still kept to S3 URLs (the amendment on web addresses), as a second guard.

Source: [What query language(s) do users write?](../../.scratch/logs/issues/11-query-language.md).
