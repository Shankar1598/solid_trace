# Unrestricted SQL queries behind an off-by-default instance setting

SQL queries run raw DuckDB SQL exactly as written, with no validation, sandbox or Project scoping, but only on installs whose operator turns on one instance-level setting (off by default). Research showed that making untrusted SQL safe on DuckDB needs structural isolation (a separate instance or process seeing one Project's data), because instance-wide settings, statements that run during prepare, `main.<table>` and `query()` all escaped lighter checks; building that was judged not worth it for v1. Instead, the operator opts in knowing that any member can then read every organization's Logs and Events and server files, write files, drop tables and starve ingest. Don't add partial checks (AST allow-lists, scoping CTEs) as if they made it safe: they were shown not to.

## Amended: SQL queries are read-only (2026-10-17)

Before an SQL query runs, EventStore has DuckDB parse it with `json_serialize_sql`, the SQL bound as a parameter so that none of it runs, and refuses it unless it is exactly one SELECT that calls none of the functions that change state from a SELECT: `query`, `query_table`, `json_execute_serialized_sql`, the logging, profiling, PEG parser and checkpoint functions, `write_log`, `nextval` and `setseed`. DuckDB's extension autoinstall and autoload are off on every install, so a query can't load an extension whose functions write. A refusal says "SQL queries are read-only: … isn't allowed".

This came with the MCP server, whose `run_sql` tool lets an agent run SQL that Log text written by senders may have steered. It is a narrow check, not the sandbox this record rules out: SQL can't change data, settings or files any more, but it can still read every Organization's Logs and Events, files on the server and the bucket, and starve ingest with slow queries. On an install with a bucket it can also send requests from the server to any web address the server can reach, such as internal services or cloud metadata, and read the answers. So the setting stays off by default, and it still makes sense only where the operator trusts every member of every Organization. The check follows DuckDB 1.5.6's functions; a DuckDB upgrade should look for new ones that change state.

## Consequences

- Users query a stable `logs` view, not the storage tables, so storage layout changes don't break their SQL.
- Since [ADR 0005](0005-logs-per-project-hot-tables-promote-at-ingest.md) the `logs` view shows only the explorer's Project and time range. That is a convenience, not Project scoping: the SQL still runs unvalidated and can read the raw tables.
- If SQL must later be offered on installs with untrusted members, the fix is the structural sandbox, and existing cross-Project SQL will break.
- On an install with an `object_store` bucket ([ADR 0008](0008-log-parquet-in-a-bucket.md)), SQL mode can read the bucket through the instance's DuckDB secret, which every connection shares. Any `s3://<bucket>/…` URL works, including other Projects' Log files and the Event archive. Since SQL queries are read-only, it can't write there: only `COPY` writes through httpfs. The httpfs extension that reads the bucket also reads `http://` and `https://` URLs, so SQL can fetch any web address the server can reach. Without a bucket httpfs isn't loaded, and those URLs fail.

Source: [What query language(s) do users write?](../../.scratch/logs/issues/11-query-language.md).
