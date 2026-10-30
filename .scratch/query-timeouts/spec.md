# Query timeouts

**Status:** claimed

**Tickets:**
- [01](issues/01-thruster-write-timeout.md): let a timed-out run's answer through Thruster, resolved.
- [02](issues/02-read-budget.md): size a Query before running it, refuse with narrower alternatives, `force`, resolved.
- [03](issues/03-refusal-in-the-explorer.md): the Logs explorer shows the refusal, with Run anyway, resolved.
- [04](issues/04-heavy-read-slots.md): one or two heavy reads at a time, resolved.
- [05](issues/05-volumes-in-list-projects.md): live volumes in `list_projects`, resolved.
- [06](issues/06-planning-stalls.md): planning stalls: cause found (Attribute sizing), fix written, claimed.

Planned with the user on 2026-10-30, after checking the slowest Queries on production. In three days, 18 Log reads hit the 30 s timeout, nearly all of them agents' Queries over MCP. The user wants no timeouts, and asked whether to change ClickHouse's sort key or to give agents better instructions.

## Problem Statement

An agent asking a question of a busy Project's Logs often gets no answer. Its Query runs for 30 s and is stopped. Usually the agent doesn't even see the timeout's advice to shorten the range, only a bare `Error POSTing to endpoint:`. While it runs, the Query takes CPU that ingest and everyone else's reads need, on a VM whose 2 vCPUs are already busy with ingest (load average 3.7–4.8). Agents then retry, or fire several Queries at once, which makes things worse.

## Findings (production, 2026-10-27 to 30)

From ClickHouse's `query_log` on the VM. Each Log read carries a `log_comment` naming its Project and kind (`event_store/storage/logs_read_tag.go`).

The install: `tagalys` takes about 90M Logs a day (about 3.7M an hour) over many files. `tagalys-verbose` takes about 265M a day (about 11M an hour) and about 57 GB stored a day, all of it `indexer` / `production.log`. The `logs` table has 176 active parts, 121 of them in the GCS bucket.

| Cause | Timeouts | What it looks like |
|---|---|---|
| **Big scans** | 9 | `search "…"` or `message contains` across every file over 1–14 days, such as `search "ShopifyCollectionUpdatesWorker" \| summarize count() by file, region_code` over 4 days. On average each read 14 GiB and 52M rows and took 18.5 s of CPU. No sort key helps a substring search across all files. |
| **Planning stalls** | 6 | Tiny reads (about 60k rows, 3 MiB) that spend 10–80 s in ClickHouse's planning (`QueryPlanOptimizeMicroseconds`), making 400–2,900 GCS GETs one at a time (about 38 ms each) across nearly every part in the table. Example: `where hostname == "t2-r3-ind-1" and pid == 13970 and file == "sidekiq.log" and worker_class == "AffectedMpagesPlatformUpdateWorker"` over 90 seconds. The same SQL later ran in 0.13 s, and in 1.6 s with the filesystem cache bypassed. The cause: ClickHouse sizes each Attribute the read names in every part, reading the part's JSON structure without the filesystem cache, and a few GETs per part add up. It happens once per Attribute and part, so the first Query to name an Attribute pays (06). |
| **Cold bucket reads** | 3 | 0.5–1.5 GiB pulled from GCS at about 50 MB/s for older days. |

Agents also ran Queries in parallel. On 10-27 at 15:20:10, five copies of one Query over different 6-hour windows of `tagalys-verbose` ran together, and all five timed out.

**Why agents see `Error POSTing to endpoint:`.** Console runs behind Thruster (`console/Dockerfile:93`, `./bin/thrust ./bin/rails server`), with no `HTTP_WRITE_TIMEOUT` set, so Thruster's default of 30 s applies. Console's run timeout is also 30 s (`RUN_TIMEOUT_SECONDS` in `console/app/controllers/concerns/event_store_proxy.rb`). A timed-out run answers a few milliseconds after Thruster has closed the connection. Caddy logs `EOF`, and the client gets an error with an empty body.

Seen on 2026-10-30: Rails logged `POST /mcp 200, 30006.7 ms` at 08:35:03.159, and Caddy logged `EOF` for the same request at 08:35:03.161. The Logs explorer, Dashboard Charts and Monitor previews go through the same Thruster with the same 30 s limit.

## Solution

1. **Let the timeout's answer through (01).** Thruster's write timeout rises above the 30 s run budget, so a timed-out run's advice reaches the agent or the browser.
2. **Size each Query before running it (02).** EventStore estimates how much a Query would read from the counts it already keeps, without reading `logs`.
3. **Refuse an over-budget Query at once, with the narrowing done for the agent (02, 03).** The refusal gives:
   - the range's volume and the budget;
   - the largest range that fits;
   - the busiest files in the range, to filter on;
   - a narrower Query ready to run.

   Agents follow a concrete next step more reliably than a rule in a tool description.
4. **`force` (02, 03).** `force: true` over MCP, or Run anyway in the explorer, skips only the size check. The Query keeps the 30 s timeout, takes a heavy-read slot, and runs at lower ClickHouse priority. The refusal doesn't mention `force`. The tool description says to use it only when the user asks for the Query to run as written.
5. **One or two heavy reads at a time (04).** EventStore runs at most one or two heavy reads at once. Past that, it refuses at once with "another large Query is running; run them one at a time". Small reads, such as most Dashboard Charts, skip the cap.
6. **Live volumes for agents (05).** `list_projects` gives each Project's Logs and bytes per hour over the last 24 h, and its number of files, from the counts. The Project description stays the user's own text.
7. **Stop the planning stalls (06).** EventStore's connection and the SQL users' profile set `allow_calculating_subcolumns_sizes_for_merge_tree_reading=0`. Each Attribute is then sized as the whole `attributes` column, so planning reads nothing from the parts. The cost is about 10–20% more CPU for scans that read Attributes (smaller first blocks). A week of the watch query after the release shows whether the stalls stopped.

## Implementation Decisions

- **The estimate reads only the counts tables.**
  - `log_source_counts` has Logs and bytes per Project, day, service and file.
  - `log_minute_counts` has Logs per Project and minute.
  - For a range inside a day, the day's per-file figures are scaled by the share of the day's Logs that fall in the range.
  - The Query's `service` and `file` conditions (`==`, `in`) pick the files. Other conditions don't narrow the estimate, because ClickHouse still reads those rows.
- **What the budget counts.**
  - A Query that matches text in `message` or `search_text` (`search`, and `contains`, `startswith` or `=~` on `message`) is limited by bytes.
  - Any other Query, plain Log lists and tables, is never refused. It logs its estimate in Logs, so a limit can be calibrated later. A plain Log list reads only the newest page, so it's cheap at any range (decided 2026-10-30, after a review found a Logs limit would refuse "Last 7 days" on `tagalys`).
  - 02 calibrates the bytes limit from `query_log`, so that reads that finish today aren't refused. The 3-day data puts the bytes line between about 4–9 GiB (finished, in 8–14 s) and about 13 GiB and up (timed out). The bytes in `log_source_counts` are kept (decoded) bytes, close to ClickHouse's `read_bytes`.
- **The refusal is an answer, not an error page.** MCP returns it as the tool's text. The explorer shows it in place of results. It never mentions `force`.
- **`force`.** It is a run parameter on EventStore's query API and an argument of MCP `query_logs`. It skips the estimate only. A forced read is a heavy read, and it runs with a lower ClickHouse `priority`, so ingest and Dashboards keep the CPU. `CallTimings` already logs each tool call's arguments, so agents forcing on their own will show in Console's Logs.
- **Heavy reads.**
  - A heavy read is a text-match read that the estimate puts over a share of the bytes budget (for example a quarter), or any forced read.
  - The cap lives in EventStore, beside ClickHouse, and covers the whole instance.
  - Past the cap, a run is refused at once, not queued: an MCP call can't wait long, and a queue would outlast the client's limit.
  - The cap isn't `ChartRuns` (`console/app/services/chart_runs.rb`). That one guards Console's Puma threads, counts each Console process separately, and can't tell heavy from small.
- **Not rate limits.** Many small Queries in a row are harmless; the damage comes from heavy reads running together.
- **The Thruster fix.** Set `HTTP_WRITE_TIMEOUT=60` on the console service in `deploy/docker-compose.yml`, and keep the 30 s run budget.
- **Volumes come from the counts, not the description.** A description would go stale as volumes change.

## Open Questions

- **Do Dashboard Charts and Monitors get refused too?** Proposed: not at first. Their Queries are saved and mostly small. For them, EventStore logs the estimate without refusing, until the logs show the budget wouldn't refuse a Chart that works today.
- **The budget's exact values** (02 calibrates them).

## Out of Scope

- **A new sort key.** `ORDER BY (project_id, service, file, time)` already makes filtered Queries fast. It can't help a substring search across all files, and the planning stalls happen before any data is read.
- **A full-text index for `search`.** `search` matches case-insensitive substrings, which ClickHouse's skip indexes can't serve. A token index would change what `search` matches, and would add indexing CPU on a box with none spare.
- **Background runs for huge scans** (a long timeout, low priority, a run id to check later). None of the 9 big scans needed one: each had a narrower form that answered its question.
- **Searching in slices.** A Logs-returning `search` could scan newest-first in slices that fit the budget, and return the matches so far with a cursor. Worth doing if refusals prove too blunt for needle searches.
- **Splitting `tagalys-verbose` by `file`.** Its Log transform could set `file` differently for ES and SQL lines, so the sort key can skip one of them. That's a decision about the user's data.
- **Estimating SQL queries.** `run_sql` keeps its own 10 s limit. Estimating arbitrary SQL would need ClickHouse's `EXPLAIN ESTIMATE`.
- **A larger bucket cache for cold reads.** `tagalys-verbose` alone moves about 57 GB a day through it, against the cache's 10 GiB.

## Further Notes

- **How the data was read.**
  - SSH to the VM (internal IP `10.164.0.3`, zone `europe-west4-b`) through the user's helper.
  - Then `sudo docker exec -i solid-trace-clickhouse-1 clickhouse-client --port 4200 --readonly=2`, on database `solidtrace`.
  - `part_log` and `text_log` aren't enabled; the server log is only in `docker logs`, without trace lines.
  - Scripts are in `~/.cache/solid-trace/query-timeouts/`.
- **Probes run on 2026-10-30, each under a minute:**
  - the 90-second Query, warm (0.13 s, 0 GETs);
  - the same Query on 10-25, cold (0.8 s, 11 GETs);
  - with the filesystem cache off, with no Attributes, one Attribute and the full Query (0.3 s, 0.7 s and 1.6 s; 6, 14 and 51 GETs);
  - with the deserialization-prefixes cache off as well (1.5 s, 51 GETs);
  - `tagalys-verbose` 90 s windows with statistics off and on (0.96 s and 0.28 s).

  None reproduced a stall: each named Attributes whose sizes the parts already held. The filesystem cache plays no part, since these reads bypass it.
- **What the stalls' ProfileEvents show.** The worst one (07:44:38, 80 s, 0 rows read) had `SharedPartsLocks=178`, about 3,400 file opens and 2,891 synchronous GETs. That's about 16 per part, close to the number of per-part metadata files (`checksums.txt`, `columns*.txt`, `*.idx`, `primary.cidx`, `statistics.packed`, `skp_idx.packed`, `serialization.json`). `LoadedStatisticsMicroseconds` was only 29 ms. No ALTER, DROP or MOVE ran before today's stalls.
- **What 06 found.**
  - **The GETs.** The 2,891 GETs were JSON serialization prefixes, read by `getColumnSizes` for each Attribute subcolumn in every part. `CachedReadBufferReadFromCacheMisses` was 0 because those reads bypass the cache.
  - **Not statistics.** Their loading took the 29 ms. Turning statistics off left a dev reproduction (1,400 GETs on 76 bucket parts) as it was. `allow_calculating_subcolumns_sizes_for_merge_tree_reading=0` took it to 0.
  - **The trail.** The source reading, the dev tables and the timings are in [06](issues/06-planning-stalls.md)'s Comments.

## Code review (2026-10-30)

`/code-review` of `feature/query-timeouts` against main (`af7b8898`), on two axes. Neither found a high.

**Fixed on `query-timeouts/review-fixes`** (the two mediums that held up, and one agent-facing low):
- **An abandoned explorer read kept the only slot.** Console keeps waiting on EventStore after the browser aborts, so a user's refined search was refused by their own old one. Now a newer explorer heavy read replaces the same user's older one: it cancels it and takes its slot. MCP reads still refuse.
- **The refusal's bar used `destructive`,** which `docs/design-language.md` keeps for errors. It now uses `warning`.
- **`list_projects` said files were "over the last 24 hours",** but EventStore counts whole UTC days.

**Lows, left for later:**
- **Formatting is repeated.** Byte, count and length formatting exists in Go (`logs_budget.go`), TypeScript (`lib/logs/refusal.ts`, beside `lib/overview.ts`'s `bytesText`) and Ruby (`query_logs.rb` `size_text`). Each layer writes its own output, and the TypeScript copy rounds down on purpose to match EventStore's message.
- **Smaller duplication:**
  - `latest`/`earliest` repeat `storage.latestTime`/`earliestTime`;
  - `topFiles` repeats the share loop;
  - `sizeUp` checks `timedOut` twice.
- **Many parameters, string keys:** `projectID, text, query, from, to` travel through `sizeUp` (8 params) and `holdSlot`. The slot key is a formatted string; a struct key would be idiomatic.
- **Type switches:** `anyOf` in `logquery/reads.go` adds an expression type switch beside `fieldName` and `hasAggregate`.
- **Unclear call sites:** `start(…, false, true)` in `pages/Logs/Index.tsx`, and the prop `refused={{…}}`.
- **Not used yet:** the histogram accepts `estimate` but no caller sends it, and `HeavyReads.Held()` exists only for tests.
- **Mixed SQL style:** `LogVolume` builds SQL with `fmt.Sprintf`, while `CountedLogs` uses `?` binds.
- **Mono and glossary:** the refusal panel puts service names, timestamps and counts in sans (design-language puts data in mono), and `top_files`/`files` hold what GLOSSARY.md calls a Source.
- **Cursor pages:** forced older pages (with a `cursor`) skip the slot.
- **Match monitors:** they aren't sized or logged.
- **Slot release:** the tests cover release on success only, not on error, timeout or a client gone. The slot frees when the handler ends, not when ClickHouse stops.
- **Shared slot for identical Queries:** two runs of the same Query and range share one slot, so they can run together.
- **Priority:** every tagged Log read now runs at ClickHouse priority 1, Charts and fields included, so that forced reads (10) yield to them.
- **Unsized reads fail open:** a Query that can't be sized (the counts read fails) runs without a slot.
