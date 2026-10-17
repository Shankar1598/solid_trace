# 12: Read-only SQL can fetch web addresses on bucket installs

**Status:** needs-triage
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
