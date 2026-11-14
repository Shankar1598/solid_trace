# Agent transcripts: raw chunks in ClickHouse, Logs for reading, and a daily sessions table

A coding agent's transcript is a directory of files: the **Agent session**'s `.jsonl`, `subagents/`, `tool-results/`, `evidence/` and others. A resume (`claude --resume`) on a new machine needs the exact bytes. Log ingest cuts strings at 64 KiB and flattens each **Log** into **Attributes**, so a Log cannot keep them. So the server stores each transcript in two layers, after it removes secrets:

1. **Raw:** one ClickHouse table, `agent_session_files`, for every path. Each row has the **Project**, the session id, the path under the session root, a version of that path, the offset, the end offset, a content hash for a file that is not `.jsonl`, and at most 1 MiB of bytes, with `ZSTD(1)`. It is `PARTITION BY (project_id, day)`, where `day` is the UTC day the server stored the unit, and `ORDER BY (project_id, session_id, path, version, offset)`. A trimmed `.jsonl` file, or another file whose hash changed, is stored again as a new version, because the table allows no delete; reads use the highest version. It is the main table of its own `DayFamily` on the `solidtrace` storage policy.
2. **Derived:** a background parse makes one Log for each `.jsonl` line, through Log ingest's `normalise`. `service` is the repository and `file` is the `.jsonl` path under the session root, so the Logs of one Agent session are one range of the sort key. `agent_sessions` has one row for each (Agent session, day of activity), in an `AggregatingMergeTree` in the raw table's `DayFamily`, summed by session id when a page reads it.

Both layers are in one **Agent Project** for each **Organization**. It holds the Agent sessions of its dispatched **Runs**, its engineers' own Agent sessions in its captured repositories, and the Runs' events. A session in any other repository never leaves the engineer's machine. They follow its **Retention**, 180 days at first.

We chose this because a resume needs the bytes, and Logs give the **Query** language, **Dashboards**, **Monitors**, Retention and Organization grants with no new work. A prototype on 75 real sessions (1.69 GB) measured it: the raw layer stores 6.6 times less than the raw bytes, the largest session (434 MB) came back equal byte for byte in 0.84 s, the Logs add 4% of the raw bytes, and each sessions-page query took less than 0.5 s.

## Considered options

- **Files written to the bucket directly:** about 17 times more objects (one for each upload, as the bucket has no append), or rewrites, and new read and delete code. In ClickHouse, a resume is one query, and Retention, Project deletion, the disk limit and the move to the bucket use the code that exists. EventStore's object-store client has no read or append.
- **An `agent_messages` table:** the sessions page met its 2-second rule on Logs, and the table would not make a text search across sessions faster.
- **Two raw tables by retention class** (tool results kept 30 days): `tool-results/` is 0.3% of the raw bytes, and most full tool output is in the `.jsonl` too, so one Retention for everything was chosen.
- **Agent sessions in the Run's own Project:** an Issue Run's MCP token reads its Project, so each Run could read the transcripts of earlier Runs. The Project's **Log transform** would also run on transcript lines, and transcript Logs would change the app's **Overview**, **Source counts** and **Services**.

## Consequences

- The download of one session reads the rows in key order with small read blocks (`max_block_size=16`, `read_in_order_use_buffering=0`): 296 MB of memory for the largest session, against 1.05 GB with the defaults. If one session's download needs more than about 1 GB, ClickHouse is the wrong place for the raw layer.
- Redaction runs on whole units (a `.jsonl` line, or a whole other file of at most 32 MiB), because a secret that two chunks split would be missed. It replaces each matched byte with `*`, and no pattern may match a quote or a backslash. So each line stays valid JSON with its length, each stored offset is the client's offset, and a resume still works.
- The parse into Logs and `agent_sessions` must be exactly once, because the sessions table is summed: each parsed range is inserted with a deterministic insert deduplication token, and the parse cursor moves after the insert.
- Old days are dropped as whole (Project, day) partitions, so the raw record and its Logs lose the same days together. After Retention a file can have no start, and the download marks it "partial".
- A text search across all sessions grows with the volume. It is a Query with a 7-day default range; a text index would be a change to Logs for every Project.
- The Overview's Biggest sources lists transcript files for the Agent Project.

Source: [.scratch/software-factory-mvp/spec.md](../../.scratch/software-factory-mvp/spec.md)
