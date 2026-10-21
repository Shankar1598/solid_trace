# 06: Probe Log file codecs: lz4_raw and zstd levels

**Status:** needs-info

**What to do:** Idea 5 of [performance-ideas.md](../research/performance-ideas.md). The user chose "probe it first" in [ticket 01](01-review-performance-findings.md), and asked to compare `lz4_raw`, zstd at its lowest level, and today's zstd. Measure each codec's file size and read times for the same hour, then let the user decide whether the export changes codec.

## Today

- The export writes `COMPRESSION zstd` with no level: DuckDB's default, level 3 (`storage/logs_export.go`, `stageHour`).
- On production, a tagalys hour is 550 to 640 MB.
- The research's estimate was that `lz4_raw` decompresses about 2.5× faster than zstd, for files 35–40% bigger. zstd decompresses at about the same speed at every positive level, so a lower level should mostly cost the export's CPU, not reads. Negative ("fast") levels trade ratio for speed both ways.

## How

- `bench/logreads` gains `LOGREADS_PROBE_CODEC` (`lz4_raw`, or `zstd:<level>`).
- It writes the exported hour's file again with that codec, keeping its rows' order and its row groups of 30,720, then runs the probe's reads on it.
- One run per codec, one at a time, pinned to 2 CPUs, 300,000 Logs.
- Synthetic Logs: absolute sizes differ from production's, so the comparison is between codecs.

## Acceptance

- [x] File size and the probe's Log file read times for zstd 3 (today), zstd 1, zstd -5, zstd's minimum level (-131072) and `lz4_raw`, in this ticket's Comments
- [ ] A recommendation, and the user's call

## Comments

### Figures (2026-10-21)

- One run per codec, one at a time, pinned to 2 CPUs, the same 300,000-Log hour.
- Raw output is in `~/.cache/solid-trace/eventstore-performance-notes/codec-runs.txt`; the script is `codec-runs.sh` beside it.
- Times are the probe's best of 3 per query, in ms. Single runs carry about ±15% noise.

| Codec | File | Size vs today | Newest page | `search` | `contains` | `contains_cs` | `contains` + `shop` | All 11 file queries | vs today |
|---|---|---|---|---|---|---|---|---|---|
| zstd 3 (today) | 54.9 MB | — | 138 | 563 | 380 | 246 | 445 | 3,407 | — |
| zstd 1 | 55.4 MB | +1% | 97 | 656 | 425 | 227 | 377 | 3,019 | −11% (noise) |
| zstd -5 | 81.7 MB | +49% | 93 | 472 | 323 | 201 | 363 | 2,558 | −25% |
| `lz4_raw` | 90.1 MB | +64% | 86 | 392 | 260 | 139 | 267 | 2,023 | −41% |
| zstd -131072 (lowest) | 161.1 MB | +193% | 88 | 369 | 221 | 135 | 246 | 1,821 | −47% |

**What the figures say:**
- **zstd 1** is the same size and read speed as today. Its only gain would be a cheaper export, which this probe doesn't time.
- **`lz4_raw`** reads files about 40% faster, for files about 64% bigger. That is a worse ratio than the research's estimate of 35–40% bigger.
- **zstd -5** is beaten by `lz4_raw`: it is nearly as big, and slower to read.
- **zstd's lowest level** is barely faster than `lz4_raw`, at nearly three times today's size.
- **What changes reads:** only file reads gain. The explorer's default load reads the hot table.

**Production's scale, if the ratio held:**
- A 600 MB tagalys hour becomes about 1 GB with `lz4_raw`.
- The 24 hours kept on local disk grow from about 14 to 23 GB.
- A month in the bucket grows from about 430 to 710 GB.
- Production's Logs compress to about 50 bytes each at zstd 3, against 183 for the probe's. So the real ratio needs a real file to check.

### On a production hour (2026-10-21, with the user's OK)

- **The file:** tagalys's hour 15 of 2026-10-21, copied over SSH. 604 MB, 14.7M Logs, 478 row groups, 104 columns.
  - `message` is 372 MB of it, compressed from 3.1 GB; `id` is another 175 MB.
- **The slice:** its first 2M Logs, written with each codec, then timed with the Query language's SQL on 2 pinned CPUs and 2 DuckDB threads, best of 3.
  - `contains` is `contains(lower(message), …)`, and `contains_cs` is `contains(message, …)`.
- **Afterwards:** the copy and the slices were deleted; only the timings are kept (`~/.cache/solid-trace/prod-hour-codec/reads2.txt`).

| Codec | 2M Logs | Size vs today | Write | `contains` | `contains_cs` | Newest 200 | Read all of `message` |
|---|---|---|---|---|---|---|---|
| zstd 3 (today) | 89.6 MB | — | 3.9 s | 616 ms | 222 ms | 140 ms | 272 ms |
| zstd 1 | 91.2 MB | +2% | 3.5 s | 594 ms (−4%) | 208 ms | 129 ms | 278 ms |
| zstd -5 | 141.3 MB | +58% | 3.5 s | 529 ms (−14%) | 190 ms | 111 ms | 239 ms |
| `lz4_raw` | 150.9 MB | +68% | 3.5 s | 475 ms (−23%) | 137 ms (−38%) | 95 ms (−32%) | 187 ms (−31%) |
| zstd -131072 (lowest) | 551.0 MB | +515% | 3.7 s | 481 ms | 119 ms | 78 ms | 167 ms |

- **zstd 1:** writes about 10% faster, for 2% bigger files; reads don't change.
- **`lz4_raw`:** the default search, `contains`, which is case-insensitive, gains only 23%, as `lower()` costs more than decompressing. Files grow by 68%:
  - a production hour, from 604 MB to about 1 GB;
  - the 24 hours on local disk, from about 14.5 to 24 GB;
  - a month in the bucket, from about 435 to 730 GB.

**Recommendation: keep zstd 3.**
- A 23% gain on case-insensitive searches over files doesn't pay for 68% more disk and bucket, and the explorer's default load reads no files.
- If searches over older hours become the slow path, a skip index (idea 7) would gain far more on rare terms.
- zstd 1 is a fair small change if the export's CPU ever matters. It saves about 10% of writing, for 2% bigger files.

Waiting on the user's call.
