# 10: How are logs stored and retained?

Type: grilling
Status: open
Blocked by: 05, 09
Map: [SolidTrace Logs](../map.md)

## Question

Given the research and the benchmark, which storage layout do logs use, where hot and cold data live, what the unit of retention is and how per-Project retention and disk budgets work at 3–5 TB/month, and how much burst headroom above the average ingest rate the design must absorb? (The engine line is already decided: DuckDB 2.0; see the map's Notes.)

Input (2026-10-02, from [What query language(s) do users write?](11-query-language.md)): whatever layout is chosen must expose a stable `logs` view (`project_id`, the seven built-in fields, the Attributes) for SQL queries, and changes to the layout must not change that view.
