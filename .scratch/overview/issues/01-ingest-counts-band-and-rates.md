# 01: Ingest counts, the band and rates

Spec: [../spec.md](../spec.md)

**What to build:** A member opens Overview, first in the sidebar and listed in the command palette, and sees their Organization's Today, 7 days or 30 days (whole UTC days, kept in the URL).

- **The band:** Received → Stored. Received is the bytes and Logs received; Stored is the compressed bytes ClickHouse holds for the range's days, local and bucket. Each step is as wide as its bytes, with the compression ratio named. Unreadable Logs and requests turned away as overloaded are shown beside it.
- **Rates** under the band: Logs, bytes and requests per second, each as an average and a peak with the second it happened. The Organization's peak sums its Projects within each second. Today adds the last five minutes' Logs per second.

End to end:

- Log ingest counts every request in memory, per Project and arrival second, including requests whose Logs are all dropped and requests turned away. That's Logs received, bytes received (after decompression), bytes on the wire, requests, unreadable Logs and overloaded requests.
- An ingest-owned flusher writes the seconds that have ended into a new `log_ingest_counts` summing table about once a second, and retries a failed write with the same rows and dedup token.
- The table is partitioned by Project and day and joins the day tables, so Retention, Project deletion and the disk limit drop it.
- A new `POST /overview` on EventStore's loopback Query API takes the Project ids, the days and whether to include Storage, and answers from Ingest counts and ClickHouse's parts. It never reads `logs`.
- Console's answer builder makes the sections for both the page and a new `get_overview` MCP tool (range `today`, `7d`, `30d`), which ends with a link to the page. The tool is registered, documented in the MCP docs and named in the server instructions.

The page follows the spec's look:

- The band fills once on first load, skipped under reduced motion.
- Rates are plain lines, and numbers use tabular figures.
- Light and dark tokens, and one column on a phone.
- Today refreshes every minute while the tab is visible.
- An Organization with no Logs gets an empty state linking to Projects.
- When EventStore can't be reached, the page says so and renders what Console holds.
- When Ingest counts start after the range does, the page says so.

Add **Overview**, **Ingest counts** and **Source counts** to GLOSSARY.md.

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] Logs sent to two Projects at chosen seconds on the fake clock come back from `/overview` as the right received, unreadable and overloaded counts, average and peak rates with their seconds, and an Organization peak that sums Projects in a second (Go route test)
- [x] Stored bytes come back by day from ClickHouse's parts, local and bucket (Go route test; the bucket half skips on the dev box, which has no MinIO env)
- [x] A flush that fails and is retried doesn't count twice; ingest makes no insert per request (Go route test)
- [x] Retention and Project deletion remove a Project's Ingest counts with its Logs (Go route test)
- [x] The page's props cover only the member's Organization, the range comes from the URL and defaults to Today, and EventStore unreachable still renders the page (Rails controller test, EventStore stubbed)
- [x] `get_overview` answers the same band and rates with a link (MCP tool test)
- [x] A Playwright flow sends Logs to two Projects, opens Overview from the sidebar and sees the band's steps and the rates, then switches to 7 days
- [x] The route answers in under 300 ms on the dev stack's data

## Answer

Built on `overview/01-ingest-counts-band-and-rates`, merged into `feature/overview` as `3317f09` (2026-10-24). Extension points for 02 to 06 are in `~/.cache/solid-trace/overview-notes/notes.md` under "01 result".

Tests run on the merge:
- Go: `go test -p 1 -run 'Overview|IngestCounts|Overloaded' .` passed.
- Rails: the Overview controller and MCP tool tests, 12 runs, 0 failures.
- `tsc` is clean.
- The Playwright flow `e2e/flows/overview.spec.ts` passed 2 of 2 on the implementer's own stack.
- `/overview` answered in 13 to 26 ms on dev data.

Open:
- The bucket stored-bytes test skips on this box (no `~/.cache/solid-trace/minio/env`).
- The route's speed on production's 30 days of per-second rows is untested.
- On dev data Today's ratio reads about 250×, because Received counts by arrival and Stored by the Logs' own day. 03's kept over stored ratio fixes this.
