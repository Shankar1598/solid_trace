# Log files are listed in a manifest in logs.duckdb

Status: superseded by [ADR 0016](0016-logs-and-events-in-clickhouse.md) (Logs and Events in ClickHouse). No new Log files are written; the manifest of those written before the switch is loaded into ClickHouse, which reads them until Retention deletes them.

Which Log files exist is recorded in `log_files`, a table in `logs.duckdb`, and not found by listing folders, even when every file is on local disk. The hourly export inserts a file's row in the transaction that deletes the file's Logs from DuckDB, so the row is the export's commit marker: at startup a staged file with a row is renamed, and one without is deleted. Reads, Retention, Project deletion and the disk limit go only by rows. We chose this because Log files are moving to a bucket, where a listing is slow and can't commit with DuckDB; because a read then takes its files from the same DuckDB snapshot as its hot table, so a Log is never in both or in neither; and because facts about each file (its size and time range, and later the highest Log batch it holds) are wanted without reading its footer.

## Considered options

- **Listing folders, as [ADR 0004](0004-logs-hot-table-and-hourly-parquet.md) and [ADR 0005](0005-logs-per-project-hot-tables-promote-at-ingest.md) were built:** no extra state, but recovery read a Log id from each staged file to learn whether its export committed, the disk limit walked the disk on every pass, and a bucket would need a listing per read.
- **DuckLake:** rejected in ADR 0005 for reading keys through `coalesce` and for mixing local and bucket locations.

## Consequences

- Deleting a file's row and deleting its bytes are two steps. Retention, Project deletion and the disk limit delete rows and leave a tombstone for each (`log_file_deletions`) in one transaction. Each deleter then deletes the bytes of every tombstoned file that no read has pinned, and its tombstone; a pinned file waits for a later pass, and Cleanup runs one every minute while any file waits.
- A read pins its files by their logical key, `project=<id>/day=<d>/hour=<h>/<name>`, not by their path, so a pin holds wherever the bytes are.
- A late file's name comes from the names its hour has in the manifest, tombstones included, so a new file never takes an old one's name.
- An install from before the manifest gets its rows once, at startup, from the files on disk; a marker row (`log_file_backfill`) records that it is done. A file it can't read is listed with only its size, so its name stays taken and the deleters still delete it.
- After that, folders are not the record: a file put on disk by hand is ignored, and a backup of the Log files must be restored together with `logs.duckdb`.
- Each row has a generation from a sequence, which no other row ever has. So an hour's generations change whenever its Logs do: a late file adds one, and Retention, Project deletion or the disk limit takes some away; moving a file's bytes to the bucket changes none. The bin cache keys a chart's bins by the generations of the hours they cover, so it never serves a bin whose Logs have changed.
- With a bucket ([ADR 0008](0008-log-parquet-in-a-bucket.md)), a row's `local` and `remote` say where the file's bytes are, and a read takes each file from its local path or its bucket URL. A tombstone then stays until the file's object is deleted too.
