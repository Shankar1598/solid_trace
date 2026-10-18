# 10: The Issues page's Users column always shows 0

**Status:** resolved
**Area:** Console, EventStore
**Found:** 2026-10-18, reported by the user

## What happens

The Issues page's Users column is hard-coded: it renders `format(0)` for every Issue (`console/app/frontend/pages/Issues/Index.tsx`). It isn't a count that fails to load; no count exists.

## What records users today

Nothing. Checked on 2026-10-18 at `6151a81`:

- **Console:** `issues` has `times_seen`, `first_seen_at` and `last_seen_at`, which Event processing keeps, but no user count. `IssueSerializer` sends none.
- **EventStore's Event index:** `events_hot` in DuckDB holds `uuid`, `project_id`, `issue_fingerprint_id`, `timestamp`, `environment`, `server_name`, `release`, `level` and `tags` (`event_store/storage/duckdb.go`). There is no user column.
- **Tags:** `extractTags` (`event_store/processing/extract.go`) copies only the payload's `tags` object. The Sentry payload's `user` object (id, email, username, IP address) is never read, so a user shows up in `tags` only if the sender sets a tag for it.
- **Raw payloads:** each Event's whole payload is kept in Pebble (`storage/pebble.go`), so the `user` object is stored, but nothing reads or counts it.

So showing a real count means building user tracking: reading the payload's `user` in Event processing, keeping each Issue's distinct users somewhere (a DuckDB column and a `COUNT(DISTINCT …)`, or a counter Event processing keeps in `issues`, which ADR 0001 would make an EventStore change too), and choosing what makes two Events the same user (id, then email, then IP, as Sentry does?).

## Questions for the user

- Build user tracking, as above? If so, which identity counts as a user, and should Events with no `user` object count by IP address?
- Or, until then, hide the Users column, or show a dash instead of 0?

Nothing is changed until then.

## Comments

### 2026-10-18 resolved

The user chose to hide the column for now. The Issues page has no Users column; it showed a hard-coded 0. User tracking isn't built. If "users affected" is wanted later, the section above says what it takes.
