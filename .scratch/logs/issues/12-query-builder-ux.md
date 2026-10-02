# 12: How do the Builder, APL and SQL modes relate?

Type: prototype
Status: open
Blocked by: 06, 11
Map: [SolidTrace Logs](../map.md)

## Question

How does the visual query builder work, and how does a query move between Builder, APL and SQL modes (round-trip, one-way, or separate)? React to a rough prototype in the explorer layout.

Input from [How should the Logs explorer be laid out?](06-logs-explorer-layout.md) (2026-10-02): the chosen layout drives queries from filter chips and a search box, with the query text hidden behind a toggle. This ticket decides where the Builder, APL and SQL modes sit in that bar.

Input (2026-10-02, from [What query language(s) do users write?](11-query-language.md)): the modes are the KQL-style query language and SQL queries (only when the instance setting is on). Filter chips and search already compile to query text, which is always the real query. Decide whether chips can represent any Query or only a subset, and how an edited Query that chips can't show is presented. There is no APL mode by that name.
