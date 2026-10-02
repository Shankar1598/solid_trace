# M1-07: Promotion at ingest and the daily set

**What to build:** common Attributes become columns of their own automatically, so filters on them stay fast, without changing what any query returns. Spec: [Log storage](../spec.md#log-storage-eventstore) (Hot tables, Promotion); [Log processing](../spec.md#log-processing-eventstore) steps 1, 2 and 4; [ADR 0005](../../../docs/adr/0005-logs-per-project-hot-tables-promote-at-ingest.md).

**Blocked by:** M1-01.

**Status:** ready-for-agent

- [ ] Up to 100 (type, key) pairs per Project are promoted at ingest (never `attrs_json` keys; built-in fields don't count): `ALTER TABLE ADD COLUMN` named `"<map>.<key>"` plus a promoted-set row (Project, day, type, key, time added), in the batch's transaction. A promoted value is stored in its column and not its map. Past 100, keys stay in the maps.
- [ ] A Log's day is the UTC day of its `time`; each (Project, day) has one promoted set.
- [ ] Popularity: Logs carrying each key per Project per UTC day, kept as a top-K of about 1,000 keys, updated in the batch's transaction.
- [ ] Daily set: when the first Log of a new day arrives, the new day's set is yesterday's unchanged if it wasn't full, else the top 100 keys from the most recent day with counts; its `ALTER TABLE`s are applied in that transaction.
- [ ] Each hot table's appender is reopened after any `ALTER TABLE` on it. The promoted-set table is cached in memory by EventStore.
- [ ] The latest-N query returns promoted values merged back as Attributes, so callers can't tell what is promoted.
- [ ] Tests (driven by the controllable clock): the same Logs read back identically before and after their keys are promoted; promotion stops at 100; a key promoted partway through a day; the daily re-rank picks the top keys when the set was full and keeps the set when it wasn't. Demoted-column drops are M1-08.
