# 10: How are logs stored and retained?

Type: grilling
Status: open
Blocked by: 05, 09
Map: [SolidTrace Logs](../map.md)

## Question

Given the research and the benchmark, which storage layout do logs use, where hot and cold data live, what the unit of retention is and how per-Project retention and disk budgets work at 3–5 TB/month, and how much burst headroom above the average ingest rate the design must absorb? (The engine line is already decided: DuckDB 2.0; see the map's Notes.)

Input (2026-10-02, from [What query language(s) do users write?](11-query-language.md)): whatever layout is chosen must expose a stable `logs` view (`project_id`, the seven built-in fields, the Attributes) for SQL queries, and changes to the layout must not change that view.

Input (2026-10-02, from [Benchmark the shortlisted storage layouts at reduced scale on the dev box](09-benchmark-storage-layouts.md)): the benchmark now runs about 5 minutes per layout on the dev box, so it gives rankings and settings for the hot-table layout and Parquet file shape, not proof of 3–5 TB/month; S3, DuckLake and the full-scale run are parked as map fog. If DuckLake is chosen, prefer a SQLite catalog in its own file over a DuckDB-file catalog: it lets a separate sandboxed SQL-mode process attach (see [the SQL sandboxing research](04-apl-kql-surface-and-sql-sandboxing.md)), at the cost of bundling DuckDB's `sqlite` extension.

Input (2026-10-02, from [the reduced-scale benchmark](09-benchmark-storage-layouts.md), findings in [research/09](../research/09-storage-bench-reduced.md)):
- **Hot table:** typed MAPs rank first. JSON text is the fallback if a simpler write path matters more than up to 5× faster Attribute queries and about 30% less disk.
- **VARIANT:** not on the 2.0 alpha. Re-check on 2.0.0.
- **Cold tier:** Parquet from the MAP layout, 30k-row row groups. Async read-ahead buys nothing on local disk.
- **Day-long Attribute searches:** need time-bounded defaults or pruning beyond row-group stats (projected 2–4 min per day on 2 cores).
- **Disk:** about 28–30 GB/day of Parquet and 38 GB/day of MAP hot table at 1.9 MB/s.
- **Bursts:** automatic checkpoints stall writes for 2–3 s at 5× bursts, so plan headroom or buffering ahead of DuckDB.
