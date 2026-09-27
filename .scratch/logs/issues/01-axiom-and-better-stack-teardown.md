# 01: How do Axiom and Better Stack work for logs?

Type: research
Status: resolved
Blocked by: none
Map: [SolidTrace Logs](../map.md)

## Question

What do Axiom and Better Stack actually offer for logs, end to end, as the benchmark SolidTrace Logs is measured against? Cover: ingest API shape and auth (tokens, datasets/sources), field and payload limits, timestamp handling, how schemaless fields become fast to query (Axiom's zero-config vs Better Stack's explicit indexing over ClickHouse JSON), the APL features users actually rely on day to day, the explorer and query-builder UX, and at a glance their dashboards and monitors. Also: what would it cost SolidTrace to be Axiom-compatible (pasteable APL, Axiom ingest API) versus Axiom-inspired?

Starting points: `.scratch/logging-old/research/research-axiom.md`, `research-betterstack.md`, `research-landscape.md` (unverified). Verify key claims against primary docs and fill gaps rather than redoing them.

## Answer

Full findings: `.scratch/logs/research/01-axiom-and-better-stack.md` on branch `research/axiom-and-better-stack` (commit `8b9b98b`); read with `git show research/axiom-and-better-stack:.scratch/logs/research/01-axiom-and-better-stack.md`.

- **Ingest is alike in both:** JSON/NDJSON POST with a Bearer token, plus OTLP/HTTP. Axiom puts the dataset in the URL path; Better Stack uses one token and host per source.
- **Timestamps:** Axiom uses `_time` with auto-detected format and `timestamp-field`/`timestamp-format` overrides, and keeps `_sysTime` (ingest time). Better Stack uses `dt`, falling back to receipt time.
- **Limits:** Axiom allows 10k events per batch, 1 MB per field and 200-byte field names, with a 256 or 1,024 field cap per dataset (an event that would exceed it is rejected). Better Stack allows 10 MiB per request and per record.
- **Why queries are fast:** Axiom turns every field into its own column automatically, with no user-managed indexes. Better Stack keeps raw JSON read via `JSONExtract`, and fast charts need per-source "extract metrics" rules. Better Stack's model breaks the map's "every JSON field queryable at once" principle.
- **Better Stack's PQL is `runreveal/pql`:** Go, Apache-2.0, about 4.4k lines, 13 operators, 10 functions. It is the closest existing KQL-style-to-SQL compiler.
- **APL is a proprietary "super subset" of KQL:** no public grammar, 368 documented features.
- **Compatibility cost:**
  - Axiom-compatible *ingest* is small and bounded (2 paths, 3 content types, 3 query params). Two traps: `axiom-go` requires an `xaat-`/`xapt-` token prefix, and clients use the legacy `/v1/datasets/{ds}/ingest` path.
  - Pasteable *APL* is large and open-ended: undocumented `bin_auto`, Axiom-specific `has` term rules, `summarize | limit` semantics, and field-name assumptions.

Open questions it hands to decisions: ingest compatibility separately from query compatibility; `xaat-` prefix acceptability; baseline language (KQL, APL or a PQL-sized subset); tokeniser and bucket-sizing rules.
