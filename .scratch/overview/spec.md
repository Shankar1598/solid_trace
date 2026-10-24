# Overview

Status: ready-for-agent

Planned with the user on 2026-10-24. Terms are in [GLOSSARY.md](../../GLOSSARY.md); the new ones are listed under Further Notes.

## Problem Statement

A member can't see how much their Organization sends SolidTrace or what it costs to keep. How many Logs arrived today, how big they were, how well they compress, how busy the busiest second was, which Service or file is the noisiest, whether a shipper went quiet: none of it is on any page. The numbers exist only as ClickHouse internals or as lines in EventStore's own Logs, so answering "why is the disk filling" or "what would this cost on Datadog" means SSH and SQL.

A Dashboard can't show them either: SQL queries run as a per-Project read-only user that can't read ClickHouse's system tables, and nothing records what arrived as opposed to what was stored.

## Solution

An **Overview** page per Organization, first in the sidebar, that answers "what came in, what we kept, what it costs" at a glance, and looks good doing it.

- **The band** leads the page: one horizontal band of three steps, Received → Kept → Stored, each as wide as its bytes, with what was dropped between them and the compression ratio at the end. Under it, rates: Logs, bytes and requests per second, each as an average and a peak, the busiest second named with its time.
- **Daily Charts** for the range: Logs per day by Project, received and stored bytes per day, and the average and peak Logs per second each day.
- **Projects**: one row each, with Logs, sizes, ratio, share, rates, Retention, the oldest day kept, and when the last Log arrived.
- **Biggest sources**: the Services and files that send the most Logs and bytes.
- **Health**: how long Log processing has kept a Log batch waiting, how fast reads ran over the last day, Monitors whose last run failed, Firing Monitors, new Issues and Events.
- **Compared with other tools**: what the range's volume would cost at Datadog's, Better Stack's and Axiom's list prices, plus GCP's internet egress for sending it there.
- **Storage**, for Instance admins only: the whole install's local disk against the disk limit, the bucket, and the old Parquet files still kept.

The range is Today, 7 days or 30 days, in whole UTC days. EventStore starts recording what arrives (**Ingest counts**) and where Logs come from (**Source counts**) in two small ClickHouse tables, so these numbers begin the day it ships. Stored sizes come from ClickHouse's own record of parts and go back as far as Retention keeps Logs. The page never reads the `logs` table, so it costs about what a small Chart costs.

The same answer is available over MCP as `get_overview`.

## User Stories

### Finding it

1. As a member, I want Overview as the first item in the sidebar, so that the state of my Organization is one click away.
2. As a member, I want Overview in the command palette, so that I can reach it from the keyboard.
3. As a member, I want the page to cover only my Organization's Projects, so that I see my own volume and not other Organizations'.
4. As a member of several Organizations, I want switching Organization on the Overview to open that Organization's Overview, so that I can compare them.
5. As a member, I want to choose Today, 7 days or 30 days, so that I can see today's load or the month's trend.
6. As a member, I want the range in the URL, so that a link I share opens the same view.
7. As a member, I want the page to say its days are UTC days, so that a day's totals don't surprise me in my time zone.
8. As a member viewing Today, I want the page to refresh itself every minute while the tab is visible, so that today's numbers stay current without me reloading.

### The band

9. As a member, I want the bytes and Logs received in the range, so that I know how much my shippers sent.
10. As a member, I want the bytes and Logs kept, after Logs that can't be read and Logs the Log transform dropped, so that I know what was actually stored.
11. As a member, I want the bytes stored on disk and in the bucket for the range's days, so that I know what they cost to keep.
12. As a member, I want each step of the band as wide as its bytes, so that compression and drops are visible before I read a number.
13. As a member, I want the dropped Logs shown between Received and Kept, split into unreadable and dropped by the Log transform, so that I can tell a broken shipper from my own filter.
14. As a member, I want the compression ratio (kept bytes over stored bytes), so that I know how well my Logs compress.
15. As a member, I want requests turned away as overloaded counted beside the band, so that I know when EventStore pushed back.
16. As a member, I want the band to say when Ingest counts start after the range does, so that a short Received step isn't mistaken for a drop.

### Rates

17. As a member, I want the average Logs per second over the range, so that I know the usual load.
18. As a member, I want the peak Logs per second, the busiest single second, with its time, so that I know the worst burst.
19. As a member, I want bytes per second, average and peak, so that I know how hard the busiest second pushed EventStore.
20. As a member, I want requests per second, average and peak, so that I can see shippers that send many small batches.
21. As a member, I want the Organization's peak to be the busiest second of all its Projects together, so that it's the load EventStore actually took.
22. As a member, I want rates by arrival time, not by the Logs' own time, so that a shipper catching up after an outage doesn't fake a spike.
23. As a member viewing Today, I want Logs per second over the last five minutes, so that I can see the load right now.

### Daily Charts

24. As a member, I want Logs per day stacked by Project, so that I can see the trend and who drives it.
25. As a member, I want each Project to keep its colour whatever the range, so that I can follow it from Chart to table.
26. As a member with more than seven Projects, I want the rest folded into Other, so that the Chart stays readable.
27. As a member, I want received and stored bytes per day side by side, so that I can see what each day cost to keep.
28. As a member, I want average and peak Logs per second per day, so that I can see whether bursts are growing.
29. As a member, I want a tooltip on every bar and point with its day and values, so that I can read exact numbers.
30. As a member, I want each Chart's numbers as a table below it, as Charts elsewhere have, so that nothing depends on colour alone.

### Projects

31. As a member, I want one row per Project with its Logs, received, kept and stored bytes, ratio and share of the total, so that I know who uses the most.
32. As a member, I want each Project's average and peak Logs per second, so that I know who causes the spikes.
33. As a member, I want each Project's Retention and oldest day kept, so that I know how far back its Logs go.
34. As a member, I want each Project's total stored Logs and size across every day kept, so that I know its whole footprint, not just the range's.
35. As a member, I want when each Project's last Log arrived, marked when it's over an hour ago, so that I spot a shipper that quietly stopped.
36. As a member, I want a Project with no Logs yet to say so and link to its setup, so that I know what to do next.
37. As a member, I want a row to open that Project's Logs, so that I can look closer.

### Biggest sources

38. As a member, I want the Services and files that sent the most Logs in the range, so that I know where the volume comes from.
39. As a member, I want the same ranked by bytes, so that I find the few large Logs that cost more than many small ones.
40. As a member, I want each source's share of the total and its Project, so that I can judge whether it's worth filtering.
41. As a member, I want how many of a source's Logs the Log transform dropped, so that I can see my filters working.
42. As a member, I want a source to open the Logs explorer filtered to its Service and file, so that I can see what it sends.

### Health

43. As a member, I want how long the oldest waiting Log batch has waited, per Project, so that I know when Logs are slow to become readable.
44. As a member, I want the median and 95th percentile time of Log reads over the last 24 hours, per Project, so that I know whether the explorer and Charts are slowing down.
45. As a member, I want the slowest recent reads with their kind, Project and time, so that I can find what's slow.
46. As a member, I want Monitors whose last run failed and Monitors that are Firing, linked, so that I can fix or look at them.
47. As a member, I want new Issues and Events in the range, so that errors sit next to Logs.
48. As a member, I want each health item to wear a status colour with an icon and words, so that I don't need colour to read it.

### Compared with other tools

49. As a member, I want what the range's volume would cost at Datadog's, Better Stack's and Axiom's list prices, so that I know what self-hosting is worth.
50. As a member, I want the estimate to use my Projects' real Retention, so that tools that price by how long Logs are kept are priced fairly.
51. As a member, I want GCP's internet egress for sending the bytes on the wire to such a tool, so that the estimate includes the cost of getting Logs there.
52. As a member, I want the prices' date and a link to each pricing page, so that I can check them and see when they're stale.
53. As a member, I want the section to say these are list prices before discounts, and an estimate, so that I don't quote it as a bill.
54. As a member, I want a 7-day or Today range scaled to 30 days and labelled so, so that the tools' monthly prices compare like for like.

### Storage (Instance admins)

55. As an Instance admin, I want ClickHouse's local disk use against the disk limit in effect and the free disk, so that I know how close the install is to moving or deleting days.
56. As an Instance admin, I want the bytes in the bucket, so that I know what it holds.
57. As an Instance admin, I want about how many days until the disk limit at the last 7 days' growth, so that I can act before it bites.
58. As an Instance admin, I want the old Parquet files' bytes, local and in the bucket, and the day the last of them passes Retention, so that I know when the switch to ClickHouse is fully behind us.
59. As an Instance admin, I want the section marked as the whole install, so that I don't read it as my Organization's.
60. As a member who isn't an Instance admin, I want the Storage section absent, so that install-wide numbers stay with whoever runs the install.

### States

61. As a member of an Organization with no Logs yet, I want the page to say so and link to Projects, so that the empty page tells me what to do.
62. As a member, I want the page to load when EventStore can't be reached, saying so in place of its numbers, with Monitors and Issues still shown, so that one outage doesn't blank the page.
63. As a member on a phone, I want the band, rates, Charts and tables to stack in one column, so that the page works on a small screen.
64. As a member using dark mode, I want the page in the dark theme's own colours, so that it's as readable at night.
65. As a member who prefers reduced motion, I want the band's opening fill skipped, so that nothing moves on load.

### MCP

66. As a member using an MCP client, I want `get_overview` with my Organization and a range, so that an agent can answer "how much did we ingest" or "what's our noisiest Service".
67. As a member using an MCP client, I want it to answer the page's sections, with the Storage section only for Instance admins, and a link to the page, so that the agent and the page agree.

### Operator

68. As an operator, I want Ingest counts and Source counts deleted with a Project's days by Retention, by Project deletion and by the disk limit, so that they never outlive the Logs they describe.
69. As an operator, I want counting to add no ClickHouse insert per request, so that ingest stays as fast as now.
70. As an operator, I want a failed write of counts retried without counting twice, so that the numbers stay true.
71. As an operator, I want the page to answer in well under a second on the production VM, so that opening it never slows the explorer.

## Implementation Decisions

### Ingest counts

- A new ClickHouse table, **`log_ingest_counts`**, one row per Project per second of arrival: `project_id`, `second` (DateTime), and summed `logs_received`, `bytes_received` (the request body after decompression), `bytes_on_wire` (the body as sent, before decompression), `requests`, `logs_unreadable` (elements or lines that aren't Logs, and Logs normalising drops), `requests_overloaded`.
- A summing table, `PARTITION BY (project_id, toDate(second))`, `ORDER BY (project_id, second)`, on the same storage policy as the others. It joins the day tables that Retention, Project deletion and the disk limit drop, so no new cleanup code. Its dedup window is set like the key counts' table.
- Rows exist only for seconds with traffic: at most 86,400 a day per Project. Per second rather than per minute so the Organization's peak is exact (the user's choice, 2026-10-24): it sums the Projects' counts in each second before taking the busiest.
- **Ingest counts in memory**, keyed by Project and second, as it answers each request, including requests whose Logs are all dropped (which write no Log batch) and requests turned away as overloaded. The second is the receipt time ingest already takes from its clock.
- **A flusher**, owned by ingest, runs about once a second. It takes only seconds that have ended out of memory into a pending write with a fresh dedup token, and inserts it. A failed write is retried with the same rows and token before anything new is taken, so a retry can't count twice. EventStore crashing loses at most the seconds not yet written; the Logs are safe in Pebble.
- No insert per request; one insert a second at most, whatever the traffic.

### Source counts

- A new ClickHouse table, **`log_source_counts`**, one row per Project, day (the Logs' own UTC day), Service and `file`: summed `logs`, `bytes` and `logs_dropped_by_transform`.
- Partitioned and cleaned up like `log_key_counts`.
- Written by Log processing in the same insert, with the same dedup token, as the key counts, so it is exact on retries.
- A Log's bytes are its message plus its Attributes' JSON, measured as stored, after the Log transform. That's also what the band calls kept bytes.
- A Log the Log transform drops counts under the Service and `file` it arrived with.

### The Overview route

- One new route on EventStore's loopback Query API, `POST /overview`. It takes the Organization's Project ids, the first and last UTC day, and whether to include Storage. Console sends the Project ids; EventStore never reads Organizations.
- It answers the band, rates, daily series, Projects, biggest sources, processing waits, read times, and (when asked) Storage, as one JSON body. Its parts run in parallel.
- **It never reads `logs`.** Its sources:
  - Ingest counts: received, unreadable, overloaded, rates, peaks and their seconds, the last five minutes, the last Log's arrival.
  - Source counts: kept Logs and bytes, transform drops, biggest sources.
  - ClickHouse's parts for `logs`: stored compressed and raw bytes and Logs, by Project and day, local or bucket; and totals across every day kept, with the oldest.
  - `log_minute_counts` is not needed; rates come from Ingest counts.
  - Log processing's in-memory state: the oldest waiting Log batch per Project.
  - `query_log`, over the last 24 hours whatever the range: read times per Project and the slowest reads.
  - Storage: ClickHouse's disks and parts, the effective disk limit worked out at start, and the old Log files module's sizes and newest day.
- **Read times need tagging.** EventStore's ClickHouse reads (newest page, Log by id, tables, Charts, the histogram, Fields, SQL queries) carry a `log_comment` naming the Project and the read's kind, so `query_log` rows can be grouped by both. It's set where reads are issued, once, not at each call site.
- Averages divide by the seconds the range has had so far (Today counts up to now). The peak is the most in any one second of the summed series, with that second.
- Budget: the route answers in under 300 ms on production's data. If a part is slow, it is precomputed, not cached.

### Console

- A new controller and Inertia page at the Organization's `/overview`, open to every member. The sidebar gains Overview first, and the command palette lists it. The landing page at `/` is unchanged.
- The controller asks the Overview route once, with a short timeout, and adds what Console holds itself: new Issues and Events in the range (Events from EventStore's existing counts), Monitors whose last run failed, Firing Monitors.
- When the route fails, the page renders Console's own parts and says EventStore's numbers are unavailable, with the reason.
- **One answer builder** (a deep module) makes the Overview's sections for both the page and MCP, so they can't drift. Its interface is "the Overview of this Organization for this range, as this user sees it". The EventStore call, Instance admin gating, scaling and the price estimates are inside it.
- The Storage section is included only for Instance admins, gated as Email settings are.
- Today refreshes every minute while the tab is visible; 7 and 30 days don't refresh.

### Compared with other tools

- **Prices live in the code** as one small, dated table: each tool's formula over GB ingested, Logs ingested and Retention days, its pricing page's URL, and the date they were checked. GCP's internet egress per GB (the tier for this volume, from a GCP region) sits beside them. Nothing is fetched at runtime.
- The tools are Datadog, Better Stack and Axiom (the user's choice, 2026-10-24).
- The volume is the range scaled to 30 days: GB received, Logs received and the Projects' Retention. Egress prices bytes on the wire. A Project without a Retention uses the instance default.
- The section shows each tool's estimate and the egress, labelled as list prices on the stated date, before discounts.
- **No saving line**: the install's own cost is left out (the user's choice, 2026-10-24).
- The prices are researched with sources when this ticket is built, not written from memory, and are updated by hand in a release when stale.

### MCP

- `get_overview`, taking the Organization's slug (optional when the user has one) and a range (`today`, `7d`, `30d`), answers the builder's sections and ends with a link to the page. It is registered on the server, documented in the MCP docs, and named in the server instructions.

### The look

The user asked for the page to look good (2026-10-24). It stays inside the Console's design system (IBM Plex Sans, the shadcn components, the validated Chart palette, the light and dark tokens) and spends its boldness in one place, the band.

- **The band** is the page's one distinctive element. A single horizontal bar, full width:
  - Received spans the width. Kept is as wide as its bytes, and the dropped slice between them is hatched, in two tones for unreadable and transform-dropped.
  - Stored is as wide as its bytes on the same linear scale, so a tenfold compression shows as a narrow block. The ratio is named beside it in large type.
  - Each step's bytes and Logs sit beneath it in large tabular figures, with its name above in sentence case.
  - A step too narrow for its label puts the label outside, with a leader line.
  - On first load the band fills once from left to right. That is the only motion on the page, and it's skipped under reduced motion.
- **Rates** sit under the band as one quiet line per measure: average, then peak with its time. Plain text, no tiles or cards.
- **Numbers** use Plex Sans with tabular figures, not monospace. Bytes are shown in binary units (KiB, MiB, GiB); counts are abbreviated (K, M) with the exact value in a tooltip.
- **Charts** follow the dataviz rules the Console's Charts already follow:
  - Bars have 4px rounded tops and 2px gaps.
  - Lines are 2px, with a crosshair tooltip.
  - One y-axis per Chart, never two. Received and stored bytes are grouped bars on one axis, since they share a unit.
  - Each Project takes a Chart colour in creation order, never cycled. The eighth Project and on fold into Other.
  - There's a legend, and up to four series are also labelled directly.
  - Each Chart has its table below.
- **Health** items use the status tokens with an icon and words, never colour alone.
- **Layout**: a single column of sections, left-aligned, at a comfortable reading width. The Charts go two across on wide screens, and everything stacks on a phone. Sections are separated by space and headings, not boxed in identical cards.
- **Copy** is plain and sentence case: "Received", "Kept", "Stored", "Logs per second". An empty state says what to do next.
- **Dark mode** uses the dark tokens' own Chart steps.

## Testing Decisions

- Good tests here check what a member, an Instance admin or an MCP client sees, given Logs sent the way a shipper sends them: the route's answer, page props, tool answers. They don't check how counts are buffered, or which ClickHouse table a number came from. The user agreed these seams on 2026-10-24.
- **Go route tests** in the root package, with the test environment's fake clock (prior art: the rate tests, which seed Logs at chosen seconds and read rates back; the retention and key-count tests).
  - Logs sent to ingest at chosen seconds across Projects come back as the right received, unreadable and overloaded counts.
  - Peaks come back with their seconds, and the Organization's peak sums Projects within a second.
  - Kept bytes and transform drops come back by source; stored bytes come back by day.
  - Retention and Project deletion remove the counts with the Logs.
  - A failed flush retried doesn't double-count.
  - Storage is absent unless asked.
- **Rails controller tests** with EventStore stubbed (prior art: the Email settings and Dashboards controller tests):
  - A member sees their Organization's sections and not another's.
  - Storage is only for Instance admins.
  - The range comes from the URL and defaults to Today.
  - EventStore being unreachable still renders Console's parts.
  - The tool estimates come from the price table with the range scaled to 30 days.
- **MCP tool tests** (prior art: the Dashboards and list_projects tool tests): the same sections, Storage only for Instance admins, and a link.
- **A Playwright flow** on the dev stack (prior art: the dashboards and logs flows): send Logs to two Projects, open Overview, see the band's three steps, the rates, both Projects in the table and a source in Biggest sources; switch to 7 days; open a source into the Logs explorer. Functional checks only.
- **A look review, once, not in the suite**: screenshots of the page in light, dark and phone width on the dev stack with seeded data, for the user to judge before merge.
- Targeted runs only; the full suites are for the user.

## Out of Scope

- The install's own cost and a saving line.
- Live prices, or tools beyond Datadog, Better Stack and Axiom, and Sentry's prices for Events.
- Backfilling Ingest counts or Source counts from before the release.
- Ranges other than Today, 7 and 30 days, custom ranges, and days in the viewer's time zone.
- An install-wide Overview across Organizations, beyond the Instance admins' Storage section.
- Compressed size per source: ClickHouse compresses columns, not sources, so it's shown only per Project.
- Alerts on these numbers (Monitors on ingest or disk), and a `/metrics` endpoint, which stays deferred.
- Changing the landing page.

## Further Notes

- The user's choices (2026-10-24):
  - The page is per Organization.
  - Biggest sources are in.
  - Ingest counts are kept per second, not per minute.
  - The band leads the look.
  - The three test seams above.
  - Tool prices for Datadog, Better Stack and Axiom, with GCP egress and no install cost, as the last ticket.
- Rates are by arrival time; the Logs explorer's `peak_rate()` stays the way to see rates by the Logs' own time. The band's Received comes from arrival seconds and its Stored from the Logs' own days, so the two can differ by Logs that arrive long after their time. The page's footnote says so.
- `query_log` is kept only a few days, so read times cover the last 24 hours whatever the range.
- New glossary terms, added to GLOSSARY.md with the first ticket:
  - **Overview**: an Organization's page of what came in, what was kept and what it costs.
  - **Ingest counts**: Log ingest's per-second counts of what arrived per Project.
  - **Source counts**: Log processing's daily counts of Logs and bytes per Project, Service and file.
- Tickets ([issues](issues/)), each a vertical slice; 02 to 06 wait only on 01, and 07 is the look review:
  1. Ingest counts, the band and rates, the page and `get_overview`; each later ticket adds its section to both.
  2. Daily Charts and Projects.
  3. Source counts, the Kept step and Biggest sources.
  4. Health, with read tagging.
  5. Storage for Instance admins.
  6. Compared with other tools, with the price research.
  7. Look review, for the user.

## Adjustments after the code map (2026-10-24)

The code map (`~/.cache/solid-trace/overview-notes/code-map.md`) found places where the code differs from what the spec assumed. These decisions replace the matching parts above.

- **Read tagging (04).** No single place issues every read, so the tag is set on the read context: in the run setup the explorer's reads share, and in Log by id and Fields.
  - Where a read also sets its own ClickHouse settings, the two are merged, since setting them replaces the whole map.
  - SQL queries aren't tagged: their per-Project read-only user already names the Project in `query_log`, and its profile refuses a `log_comment`. Their kind is "SQL query".
- **Old Parquet files (05)** show their local bytes, how many there are, and the day the last passes Retention. Their bucket bytes aren't recorded, and listing the bucket on each request would be slow, so they're left out.
- **Log transform drops (03)** are found by comparing a range's Logs before and after the transform. A range the transform empties entirely still writes its Source counts. Source counts and key counts are separate inserts under the same dedup token; dedup is per table.
- **Ingest counts (01):**
  - Bytes on the wire come from the handler and are passed through to the service.
  - A request turned away as overloaded reads the clock itself and counts only as a request turned away.
  - A request refused as too large never reaches the service and isn't counted.
- **The disk limit** picks days by the Logs' local bytes. A counts partition for a day with no local Logs stays until Retention or Project deletion drops it. That's accepted, since these tables are tiny.
- **Health (04):**
  - Firing comes from the Monitors' states, as the Monitors page derives it.
  - Events in the range are counted by the Overview route from the `events` table for the Organization's Projects, not by Console calling per Project.
- **Other details:**
  - An empty Project's setup link is its Logs page, which shows Get started.
  - `get_overview` has its own range argument (`today`, `7d`, `30d`), not the Logs ranges.
  - The Overview route is given the effective disk limit that `main.go` works out at start.
  - The test environment gains a way to set the ingest wait limit, so overloaded requests can be tested.
  - Read-time tests flush ClickHouse's logs before reading `query_log`.

## Adjustments after the code review (2026-10-24)

- **The design language wins over "The look".** Where `docs/design-language.md` differs from the look above:
  - numbers in tables are mono;
  - there are no rounded corners, bars included, as Console's other Charts have none;
  - the shared number formatters are used.
- **Colour follows the entity.** Chart colours 1 to 8 belong to Projects. The band and the bytes and rates Charts use ink and neutral tones.
- **Scaling to 30 days** divides by the seconds the range has had, not its whole days.
- **The kept-over-stored ratio** is taken over the days that have Source counts. A notice says so when the range starts earlier.
- **Axiom's storage estimate** uses ClickHouse's measured compression (raw bytes over stored bytes), and 10× when nothing is stored.
- **Days until the disk limit** project the steady-state local size: each Project's daily stored bytes × the days it keeps locally. When that fits under the limit, the page says it stays under it, with no day count.
- **Glossary:** a **Source** is a Service and file together. "Source" alone, meaning a Service, is still avoided.
