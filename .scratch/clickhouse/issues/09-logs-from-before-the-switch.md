# 09: Logs from before the switch, read in place

**What to build:** The ClickHouse release loads a prepared install's state and reads its old Log files where they are, from the bucket or the disk. Every Query over a range that reaches before the upgrade answers as it did. `deploy/install` prepares an install from the previous release's image, and EventStore refuses to start on unprepared DuckDB data. See the [spec](../spec.md)'s "Logs from before the switch" and "Cutover".

**Blocked by:** 05, 08

**Status:** ready-for-agent

- [ ] **Starting:** with `logs.duckdb` and no marker, EventStore refuses to start and names the command. With a marker, it loads the prepared files into ClickHouse once: the old Log files, the old promoted sets, the key counts, and the cursors into Pebble. It records the switch time.
- [ ] **Reads** whose range reaches before the switch time also read that range's old files:
  - with `s3()` for uploaded files and `file()` for local ones;
  - with each day's promoted set, and `coalesce(column, map[key])` for a promoted key;
  - NULL for a column a file lacks.

  This covers pages, tables, the histogram (old hours go through the bin cache, not the MV), a Log by id, and Fields from the loaded key counts.
- [ ] **Old hours' generations** change only when their files are deleted.
- [ ] **Retention and Project deletion** delete old files' objects, local copies and rows. The old S3 adapter stays for this.
- [ ] **The old path is one module** the rest calls through, so 12 can delete it.
- [ ] **`deploy/install`**, finding DuckDB data and no marker, runs `prepare-clickhouse` from the previous release's image with the stack stopped, before starting the new one. It says what it's doing and stops if the command fails.
- [ ] **Root tests**, from 08's committed fixtures:
  - Queries over the old hours, and across the switch time, answer as the same Logs would from ClickHouse;
  - a late Log of an old hour inserted after the switch is listed with them;
  - Retention deletes old files past it;
  - unprepared DuckDB data stops the start.
- [ ] Targeted tests only, `go test -p 1`.

## Comments

- 2026-10-23, review of 08: the prepared-files contract is in [08](08-prepare-clickhouse-command.md)'s Answer, and the fixtures are in `event_store/testdata/prepared_clickhouse/` (static on this branch; see its README). Two points from the review, now in the spec:
  - **Old files are chosen by their time span, not the switch time.** Prepare files the current hour and Logs up to an hour ahead, so a file's `max_time` can be later than the marker's `prepared_at`. A read takes the old files whose `min_time`/`max_time` overlaps its range (a NULL span counts as its whole hour).
  - **Load per marker, not once.** If the install is rolled back to the DuckDB release, it removes the marker on start, and a later upgrade prepares again with a new marker. EventStore records which marker it loaded and reloads, replacing the old tables, when it finds a different one.

