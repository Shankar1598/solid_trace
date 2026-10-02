# M1-07: Promotion at ingest and the daily set

**What to build:** common Attributes become columns of their own automatically, so filters on them stay fast, without changing what any query returns. Spec: [Log storage](../spec.md#log-storage-eventstore) (Hot tables, Promotion); [Log processing](../spec.md#log-processing-eventstore) steps 1, 2 and 4; [ADR 0005](../../../docs/adr/0005-logs-per-project-hot-tables-promote-at-ingest.md).

**Blocked by:** M1-01.

**Status:** ready-for-agent

- [x] Up to 100 (type, key) pairs per Project are promoted at ingest (never `attrs_json` keys; built-in fields don't count): `ALTER TABLE ADD COLUMN` named `"<map>.<key>"` plus a promoted-set row (Project, day, type, key, time added), in the batch's transaction. A promoted value is stored in its column and not its map. Past 100, keys stay in the maps.
- [x] A Log's day is the UTC day of its `time`; each (Project, day) has one promoted set.
- [x] Popularity: Logs carrying each key per Project per UTC day, kept as a top-K of about 1,000 keys, updated in the batch's transaction.
- [x] Daily set: when the first Log of a new day arrives, the new day's set is yesterday's unchanged if it wasn't full, else the top 100 keys from the most recent day with counts; its `ALTER TABLE`s are applied in that transaction.
- [x] Each hot table's appender is reopened after any `ALTER TABLE` on it. The promoted-set table is cached in memory by EventStore.
- [x] The latest-N query returns promoted values merged back as Attributes, so callers can't tell what is promoted.
- [x] Tests (driven by the controllable clock): the same Logs read back identically before and after their keys are promoted; promotion stops at 100; a key promoted partway through a day; the daily re-rank picks the top keys when the set was full and keeps the set when it wasn't. Demoted-column drops are M1-08.

## Comments

### 2026-10-02 built (logs-v1/m1-07)

What was built:
- **Promotion** (`storage/logs_promotion.go`) runs inside `LogStore.AppendLogBatches`, which now takes `now`. Log processing takes a clock: `logprocessing.New(batches, store, clock)`. `main.go` passes `clock.System{}`, and the test env passes `env.Clock`.
- **One transaction per commit:**
  1. `CREATE TABLE IF NOT EXISTS`.
  2. Each of the Logs' days, oldest first: load the day's set, or make it if it has none, then promote that day's Logs' new keys while there is room.
  3. `ALTER TABLE ADD COLUMN` for each key of those days' sets that has no column yet.
  4. Record the new set rows.
  5. Append the Logs through an appender opened after the ALTERs (`NewAppenderWithColumns`: the fixed columns plus the promoted columns of the batch's days; other columns stay NULL).
  6. Count keys.
  7. Move the cursor.
- **New tables in `logs.duckdb`:**
  - `log_promoted_sets(project_id, day DATE, type, key, added_at, partway)`
  - `log_key_counts(project_id, day, type, key, logs)`

  `type` is `string`, `number` or `bool`.
- **Read API for M1-08 and M2-01:**
  - `LogStore.PromotedSet(project, t)` and `LogStore.PromotedSets(project, from, to)`, served from memory
  - `PromotedSet{Day, Keys}` with `Lookup(type, key)`
  - `PromotedKey{Type, Key, AddedAt, Partway}` with `Column()` (for example `attrs_string.http.status`)
  - `AttributeType.Map()`, `LogDay(t)` and `MaxPromotedKeys`

  The cache is loaded whole in `NewLogStore` and updated after each commit.
- **Latest-N** selects `*` and merges every non-NULL promoted column back into its map by the column's name, so it works whatever columns a table has.

Choices the ticket left open:
- **`partway` column (an addition to the ticket's row).** "Time added" alone can't tell the compiler when to use `coalesce(column, map[key])`. The clock needn't move between transactions, and a Log's time isn't when it was processed. `Partway` is true when a key joins a day's set that already had keys before that transaction. A set made in a transaction starts with no partway keys (carried over, re-ranked, or promoted by the batch that made it), because none of the day's Logs were stored before them.
  - In practice, an earlier Log holds a partway key in its map only if it couldn't promote it then: a case clash, since a set never shrinks mid-day. So the coalesce is rarely needed, but it is always correct.
- **Popularity** is counted per (type, key). `attrs_json` keys and keys that can't be columns aren't counted.
  - The top-K of 1,000 is Space-Saving: a key new to a full top-K starts at the lowest count kept. So a key that keeps coming climbs past a burst of keys seen once, where a plain trim would evict it every time (a test covers this).
  - Only changed rows are written, through one `MERGE` fed by a query appender.
- **"Yesterday's set"** is the set of the most recent earlier day that has one, so idle days are skipped. "Most recent day with counts" is strictly before the new day. Ranking ties go by type, then key. Within a Log, keys are promoted by type, then key; across Logs, in batch order.
- **A day with no set rows counts as having no set.** A day whose Logs had no promotable key gets no rows, and its set is made again (cheaply) with its next Log.
- **An earlier day after a later one** (yesterday's first Log arriving after today's) gets its own set, made from the most recent set before it.
- **Keys that can't be columns stay in their maps:**
  - DuckDB compares column names ignoring ASCII case, so `attrs_string.User` and `attrs_string.user` clash. The first one claimed wins.
  - Keys with a NUL can't be parsed as an identifier.
  - Keys with invalid UTF-8 break `duckdb_columns()`.

  Quotes in keys are escaped.
- **Days** are `DATE` columns, bound as `YYYY-MM-DD` strings. This avoids time-zone conversion of bound `time.Time` values.
- **The cache is for readers only.** The transaction reads the sets it needs from `logs.duckdb`, so a stale cache can't corrupt the table. If `COMMIT` itself fails ambiguously, the cache can lag until the next commit touching those days.
- **Test seam:** tests observe promotion through `LogStore.PromotedSet`, the API the compiler reads, and through latest-N read-back. They don't assert on SQL or table columns.

Deviations: the extra `partway` column (above). None from the spec otherwise.

Follow-ups for later tickets:
- **M1-08:**
  - Pass only hot-window Logs to `promote`. It makes a set for every day it is given.
  - Decide whether `logs_late` Logs are counted (`countKeys` takes any Logs).
  - Demoted columns: compare `PromotedSet` of yesterday and today with the table's columns (`tableColumns`).
  - `promote` already re-adds any column a day's set needs, so drops are safe.
  - Latest-N's merge-by-column-name carries over to Parquet read with `union_by_name`.
- **M2-01:** `PromotedSets(project, from, to)` gives each day's columns. Use `Column()` for a promoted key, `coalesce` when `Partway`, and `Map()` otherwise.
- **M1-09:** Retention and Project deletion must delete `log_promoted_sets` and `log_key_counts` rows, and evict them from the cache. `promotedSetCache` has `put` only.
- **M1-06:** a failing ALTER fails the whole commit. Retrying Log by Log through `AppendLogBatches` redoes promotion each time, so isolation works without changes here.
- **M1-10:** each commit now adds:
  - a `LIMIT 0` schema read
  - one or two promoted-set reads
  - per day: a counts read (at most 1,000 rows) and a `MERGE`
  - on a new day, a counts ranking

  The cache holds every day's set of every Project (Projects × Retention days × up to 100 keys): fine for a prototype; share identical consecutive sets if it matters.
