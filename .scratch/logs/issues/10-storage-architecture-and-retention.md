# 10: How are logs stored and retained?

Type: grilling
Status: open
Blocked by: 05, 09
Map: [SolidTrace Logs](../map.md)

## Question

Given the research and the benchmark, which storage layout do logs use, where hot and cold data live, how the retention window deletes data, and how much burst headroom above the average ingest rate the design must absorb at 3–5 TB/month? (The engine line is already decided: DuckDB 2.0; see the map's Notes.)

## Input: MVP scope (decided 2026-09-27)

- **Retention:** one global window in days, set in the config file, deleting whole days. Per-Project retention and disk budgets are on the map's Future list.
- **Durability is on hold.** The user is changing Event ingest to write each Event to Pebble before replying `200`. Whether logs follow the same path is measured by the benchmark's Pebble-first variant. Settle what a `200` promises for logs only after that change lands; the layout choice does not wait for it.
- **Known failure mode to design around:** `ArchiveEventsForDate` runs `COPY TO` inside a transaction. If `COPY` succeeds and `Commit` fails, the Parquet file survives and the rows stay in the hot table, so a view over both returns them twice (`.scratch/prod-readiness/review.md` §10). A hot table archived to Parquet is a likely logs layout.
- **Constraint (from [Which production-readiness gaps must be fixed before or alongside logs?](05-prod-readiness-gaps-for-logs.md)):** log storage never writes Console's SQLite.
