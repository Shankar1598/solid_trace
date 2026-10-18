# 21: A run against a real GCS bucket

**What to do:** Spec Verification. Needs a GCS bucket and an HMAC key from the user (no cloud resources are created by agents). Then: upload, evict after local_cache_hours, Queries across local and remote hours, Retention delete and the Event archive view, following [docs/logs/object-store.md](../../../docs/logs/object-store.md). Only MinIO has been tested so far.

**Status:** needs-info

## Comments

### 2026-10-18

Also time the 12-chart, 7-day Dashboard once the hours are in the bucket, cold (after an EventStore restart) and warm, to finish the compaction re-check ([carry-over](../carry-over.md)). The local figures and the method are in [research/dashboard-and-stream.md](../research/dashboard-and-stream.md): 5.6 s cold and 3.6–4.2 s warm, Queries only. Compaction would help only if the bucket's per-file requests make the cold run much slower than that.
