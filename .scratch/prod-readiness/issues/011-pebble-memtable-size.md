# Pebble memory table size: throughput against write pauses

Status: ready-for-agent

**Source:** benchmarks run on 2026-09-26 while redesigning the §4 write path (not a `review.md` item)
**Area:** `event_store/storage/pebble.go`, `event_store/config/config.go`, `deploy/event_store.yml`

## Problem

`storage/pebble.go` opens Pebble without `MemTableSize`, so Pebble uses its 4 MB default.
That default limits write throughput. A larger memory table raises throughput but adds long write pauses.
We need data to pick the size.

This matters more after the planned write-path change. Each request will write its own Event to Pebble under a mutex and reply `200` only after the write. So a request waits through any Pebble pause.

## What the benchmarks showed

Test machine: 2-core GCP VM. Each Event is about 2.2 KB. Each write is one Pebble batch with the Event key and an Event log key, committed with `NoSync`, under a mutex.

| | 4 MB (default) | 64 MB |
|---|---|---|
| Throughput at saturation | 18.3–18.5k Events/s | 37.7–41.2k Events/s |
| Write stalls at 20k/s (15 s run) | 111, about 10 s stalled | 0 |
| Request latency at 5k/s, p50 / p99 | 14 µs / 6 ms | 25 µs / 153 ms |
| Request latency at 20k/s, p50 / p99 | 351 ms / 4.3 s (overloaded) | 19 µs / 276 ms |

The 64 MB table has pauses of about 290 ms at 5k/s, with no write stall reported. The cause is not confirmed.
The likely cause is the switch to a new memory table when the current one is full. That occurs about every 6 s at 5k/s.

Separate finding: with 4 MB, a 1000-Event batch (about 2.2 MB) is more than half the memory table. Pebble then flushes each batch as its own L0 file.
The planned one-Event-per-write path removes this problem. But it shows that the memory table size must stay large relative to the largest batch.

## Task

1. Benchmark the mutex write path at 5k/s and 20k/s with 16 MB, 32 MB and 64 MB memory tables.
   Use the setup above. Cap waiting requests at 50k, and count requests over the cap as rejected (ingest answers 503, not 429: SDKs pause for 60 s on a 429).
2. Log Pebble `FlushBegin`/`FlushEnd` and `WriteStallBegin`/`WriteStallEnd` with timestamps.
   Find out if the latency pauses align with memory table switches.
3. If the pauses align, find what makes the switch slow (for example, allocation of the new memory table, or WAL rotation). Find out if a Pebble option can reduce it.
4. Recommend a size. Add it as a `pebble_memtable_size` setting in `config.go` and `deploy/event_store.yml`, with the recommended default.

## Acceptance

- A table of throughput and p50/p99/p99.9 latency for each size and each rate.
- A confirmed cause for the pauses, or a statement that the cause is not known.
- A recommended default, and a sentence about its memory cost. Pebble can keep more than one memory table in RAM at the same time.
