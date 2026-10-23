# Logs: busy Projects' quarter-hours exported to their own files

Status: superseded by [ADR 0016](0016-logs-and-events-in-clickhouse.md) (Logs and Events in ClickHouse): there is no Log export.

Supersedes the "holding the current and previous hour, then exported every hour …, one file per Project per hour" and the "before the previous hour or at or after the next one" lines of [ADR 0005](0005-logs-per-project-hot-tables-promote-at-ingest.md).

The Log export runs at each quarter-hour start (:00, :15, :30, :45), once when EventStore starts, and a minute after a failure. Each run has a **quarter cutoff**, its quarter-hour start minus 15 minutes, and an **hour cutoff**, the start of the previous hour. A Project is **busy** at a run when its hot table holds at least 122,880 Logs (one DuckDB row group) of the quarter-hour before the quarter cutoff, so that a quarter-hour file holds about a row group; its Logs before the quarter cutoff are exported, each quarter-hour to its own `data-<mm>.parquet` (`mm` being 00, 15, 30 or 45). A quiet Project's Logs before the hour cutoff are exported, each hour to `data.parquet`, as before. Late Logs go out at every run by the same rules. Logs of a quarter-hour or hour that already has its file go to the hour's next `late-<n>.parquet`, at most one per hour per run. An hour whose first quarter-hours went out on their own keeps going by quarter-hours after the Project turns quiet, so an hour never has both `data.parquet` and `data-<mm>.parquet`.

We chose this because a busy Project's hot table, which fills in arrival order and can't be sorted, held its last one to two hours, and a Query filtered on `service` or `file` read every Log of them; on `tagalys` that was 13 to 30 M Logs. Exporting a whole hour was also one large job: 1 min 53 s to 2 min 32 s to write `tagalys`'s hour, and a 2.5 to 6 s commit holding up Log processing (production, 2026-10-21/22). A quarter-hour is about a quarter of that, and sorts in memory where the hour spilled to disk.

## Considered options

- **Every Project by quarter-hours:** a quiet Project's quarter-hour holds fewer Logs than one row group, which a filter reads whole anyway, so its quarter-hour files would only quadruple its files, bucket objects and cold-read requests (ADR 0008 counts about 3 requests per hour file).
- **Sorting the hot table, or keeping a sorted copy of it:** it fills in arrival order, every second; re-sorting it costs what exporting it does, and a copy doubles its memory.
- **A setting for the interval, the wait or the threshold:** the threshold only matters for speed, at one row group, so there is nothing to tune. Each is one constant.
- **A sub-hour column in the manifest's key, or a folder per quarter-hour:** reads, Retention, the disk limit, uploads, eviction and the bin cache all work by hour and read every file of an hour, so the quarter-hour only needs to be in the file's name.

## Consequences

- Files stay in their hour's folder, `project=<id>/day=<d>/hour=<h>/`, and the manifest's key (Project, day, hour, name) is unchanged; [ADR 0007](0007-log-files-listed-in-a-manifest.md)'s and [ADR 0008](0008-log-parquet-in-a-bucket.md)'s hour-based keys stand. The manifest sorts an hour's files `data.parquet` or `data-<mm>` by minute, then late files by number. Files already written read as before.
- Each quarter-hour's file adds a generation to its hour, so the bin cache drops the cached bins of that hour's earlier quarter-hours up to three times. Those bins couldn't be cached at all while their Logs were hot.
- A busy Project gets about four times the files, bucket objects and cold-read requests for its hours. Daily compaction (ADR 0008) stays the way to cut them if long-range first runs get slow.
- Each run checkpoints `logs.duckdb`, now four times an hour.
- Demoted columns are still dropped at the export that deletes a quiet Project's last hour of the previous day (01:00 UTC): the export works this out from the hour cutoff, so it doesn't move with the quarter cutoff.
- The hot window follows the quarter cutoff: it runs from the quarter cutoff of the run due at the time to the start of the next hour. So a hot table only holds Logs of quarter-hours not yet exported, and a run never finds a hot Log of a range that has its file. A quiet Project's on-time Logs stay hot until their hour is exported. A Log more than 15 to 30 minutes late, not one to two hours as before, goes to `logs_late`, and the next run writes it to its range's file, or the hour's late file once that file is written.
- After an outage (EventStore down, Log processing behind, or a shipper's backlog), the delayed Logs are late Logs. We accept what they lose, since it is small and passes. Their ranges usually have no file yet, so they go to that range's own file, sorted and with the day's promoted columns. Until then, at most 15 minutes for a busy Project, they are read from `logs_late`'s maps. They aren't counted for Attribute popularity and can't add a key to their day's promoted set; unless the outage is a large part of a day, the next day's set is the same. Counting late Logs is a separate change if an outage shows it matters. On production every Log arrives within a minute of its time, so the smaller window changes no count there.

Source: [Busy Projects' Logs exported every quarter-hour](../../.scratch/quarter-hour-export/spec.md).
