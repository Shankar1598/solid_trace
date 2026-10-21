# 06: Probe Log file codecs: lz4_raw and zstd levels

**Status:** claimed

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
