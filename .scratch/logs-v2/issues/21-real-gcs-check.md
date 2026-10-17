# 21: A run against a real GCS bucket

**What to do:** Spec Verification. Needs a GCS bucket and an HMAC key from the user (no cloud resources are created by agents). Then: upload, evict after local_cache_hours, Queries across local and remote hours, Retention delete and the Event archive view, following [docs/logs/object-store.md](../../../docs/logs/object-store.md). Only MinIO has been tested so far.

**Status:** needs-info
