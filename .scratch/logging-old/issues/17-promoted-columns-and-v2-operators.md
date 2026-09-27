# 17: Promoted columns at archive time and v2 operators

**What to build:** Hot Fields (frequently filtered keys) are written as real Parquet columns with bloom filters at archive time so equality filters on them skip files, and the query planner uses the column when present. The language gains `mv-expand`, `union`, `join`, `make-series` and `serialize`/`row_number`.

**Blocked by:** 10, 05

**Status:** wontfix

- [ ] Promotion list per Project (automatic from catalog frequency, editable in Project settings); archived files carry promoted columns and the manifest records which
- [ ] Planner resolves a promoted Field to the column on files that have it and to the map elsewhere
- [ ] New operators compile and pass golden and execution tests
- [ ] A benchmark shows an equality filter on a promoted Field skips non-matching files

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
