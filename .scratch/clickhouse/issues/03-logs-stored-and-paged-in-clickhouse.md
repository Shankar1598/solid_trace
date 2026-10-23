# 03: Logs stored in ClickHouse and read as pages

**What to build:** The tracer bullet. Log processing inserts Logs into ClickHouse's `logs` table, and the Logs explorer's newest page, its filters and `search`, a Log by id, Match monitors and the Fields list read them from there. See the [spec](../spec.md)'s "The `logs` table", "Log processing writes" and "Reads". After this ticket, a Log posted to a Project can be found by any filter on the Logs explorer, from ClickHouse alone.

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] **The table:** the `logs` table and the key counts table, as 01 settled them. The `file` sort column is filled from the `file` Attribute.
- [ ] **Log processing** inserts each Project's Logs about once a second, after the Log transform, up to today's batch limit:
  - the range of Log batches is recorded in Pebble before the insert, and the insert carries a dedup token from it;
  - the Processing cursor moves in Pebble after the insert, and the batches are deleted;
  - key counts go to the key counts table.

  The hot table, `logs_late`, promotion, the hot window and the Log export's timer are no longer used. Their code goes with this ticket, or with 10 if it's still referenced.
- [ ] **The commit counts line** (`kind=log_commits`) counts inserts.
- [ ] **The Query compiler** emits ClickHouse SQL for `where`, `search`, `take`, `sort by time` and the text operators, with Attributes read through JSON paths and their types kept. The newest page reads in the way 01 chose.
- [ ] **A Log by id** reads the partitions around its id's time first, then the Project, or only its time's partition when the time is given.
- [ ] **Match monitors' `after_batch`** reads Logs after the cursor in Log batch order, with the `batch` minmax index.
- [ ] **The Fields list** reads the key counts table. Its wording drops "a Log sent more than about 15 minutes after its time may not be counted", in EventStore and in the MCP `list_fields` description.
- [ ] **Root tests that pass against ClickHouse** (helpers moved off DuckDB): `logs_newest_page_test.go`, `logs_text_match_test.go`, `logs_after_batch_test.go`, the `where`/`search` cases of `logs_language_test.go`, `logs_query_test.go`'s page cases, and the Fields tests.
- [ ] **New root tests:**
  - a Log whose time is hours old is found at once by an Attribute filter;
  - a replayed range of Log batches is stored once;
  - a crash between the insert and the cursor repeats nothing and loses nothing.
- [ ] Targeted tests only, `go test -p 1`.
