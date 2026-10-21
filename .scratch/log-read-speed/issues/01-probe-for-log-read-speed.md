# 01: Probe for Log read speed

**What to build:** A probe that times the reads this effort changes, so each later ticket can show its before and after. It is gated by an environment variable, like the storage layout probes, and isn't part of the suite. It builds about 1.5M synthetic Logs for one Project: one hour in the hot table, and the same hour as a Log file listed in the manifest. The Logs have long `message`s, a few promoted Attributes, some Attributes only in the maps and a rare term in a few messages. On 2 DuckDB threads, it times what EventStore actually runs, through the LogStore and the Query compiler, not hand-written SQL:

- the newest page (200 Logs, no Query) over the hour;
- `search "term"`, `where message contains "term"` and `where message contains_cs "term"`;
- `where message contains "term" and <map Attribute> == "x"`, written in both orders.

See the [spec](../spec.md)'s Testing Decisions and Further Notes.

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] The probe runs in under a minute on the dev box, setup included, and is skipped unless its environment variable is set
- [x] It reads the hot table and the Log file through the same code paths a Query run uses
- [x] Each timing is the best of a few runs, printed with the number of Logs it found
- [x] Today's figures are recorded in this ticket's Comments as the baseline for 02 to 04

## Comments

### 2026-10-21 baseline

The probe is `event_store/bench/logreads/probe_test.go`. Run it from `event_store/`, alone on the box:

```
LOGREADS_PROBE=1 timeout 60 systemd-run --user --scope -p MemoryMax=3G --quiet mise exec -- go test -count=1 -v ./bench/logreads/
```

Add `LOGREADS_PROBE_LOGS=150000` for the smaller size. Without `LOGREADS_PROBE` it is skipped.

What it does:

- It times each Query as the Query route runs it: `logquery.Compile`, then `LogStore.ScanLogs` over the hour with the route's limit (200) and scan budget, on 2 DuckDB threads. Each time is the best of 3 runs.
- Project 2 holds the hour in its hot table, and Project 1 the same Logs as a Log file in the manifest (the probe checks both).
- The Logs have messages of 200 to 580 bytes (about 390 on average), 5 promoted Attributes (`method`, `route`, `user_id`, `status`, `duration_ms`) and 3 only in the maps (`shop`, `region`, `request_id`). One Log in 25,000 holds `zebrafish`, in lower and upper case by turns, and all of those have `shop` `s500`.
- It fails if a read finds a different number of Logs than the probe stored. So it also checks 02 to 04 don't change answers.

Decisions:

- **300,000 Logs by default, not 1.5M.** Appending through the LogStore takes about 50 µs a Log and the export about 15 µs. The reads take about 6 s a round at 300,000. A 300,000-Log run takes about 50 s wall, setup included, so 1.5M can't fit in a minute with best of 3. The figures are given at two sizes, 150,000 and 300,000, to show how they grow.
- **Seed and copies.** For each Project the probe appends the hour's first 50,000 Logs with `AppendLogBatches`, then copies them in SQL, shifted in time and with new ids, to fill the hour in time order. Then `ExportLogs` writes Project 1's file. The reads go through the real code; only the bulk of the writing doesn't.
- **A full promoted set.** A key stays in the maps only once the day's set holds 100 keys. So the hour's first Log carries 95 filler keys, and both the hot table and the file have 100 promoted columns, 95 of them almost empty. That is the shape of a busy Project's day, but it costs: without the fillers, the newest page took 114 ms at 300,000 instead of about 330.

Figures (dev box, 2 threads, ms, best of 3; Logs found in brackets):

| Read | 150k hot table | 150k Log file | 300k hot table | 300k Log file |
|---|---|---|---|---|
| Newest page, no Query | 259 (200) | 199 (200) | 330 (200) | 335 (200) |
| `search "zebrafish"` | 529 (6) | 438 (6) | 875 (12) | 754 (12) |
| `where message contains "zebrafish"` | 307 (6) | 314 (6) | 558 (12) | 523 (12) |
| `where message contains_cs "zebrafish"` | 202 (3) | 232 (3) | 363 (6) | 386 (6) |
| `where message contains "zebrafish" and shop == "s500"` | 271 (6) | 292 (6) | 461 (12) | 528 (12) |
| `where shop == "s500" and message contains "zebrafish"` | 272 (6) | 308 (6) | 476 (12) | 555 (12) |

Setup at 300,000: 11.3 s to build Project 1's hour, 4.9 s to export it, 11.1 s to build Project 2's.

Notes for 02 to 04:

- One read can vary by about 20% between runs (`contains` on the hot table was 463 ms in one 300k run and 558 in another). Compare changes larger than that, or run twice.
- Today both orders of `shop == "s500"` and the `contains` cost the same. Likely, though not measured on its own, a run of Log rows reads every column of every Log in the hour before it filters, since these conditions aren't pushed into the scan, and that read (about the newest page's 330 ms) hides the order. 02's first step reads only `time` and the condition's columns, so 04's ordering should show after 02.
