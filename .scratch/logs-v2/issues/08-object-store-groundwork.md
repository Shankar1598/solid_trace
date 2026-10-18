# 08: Object store groundwork

**What was built:** the httpfs probe (research/object-store-probe.md), the object_store config, the ObjectStore interface with an aws-sdk-go-v2 adapter and a dirStore fake, httpfs and the DuckDB secret, the Dockerfile extension, ADR 0008; tested against MinIO. Spec: [spec.md](../spec.md), Phase 5 steps 2 and 3.

**Status:** resolved

Built and reviewed as unit B4a of the Logs v2 build (implement, review and verify, fixes, recheck). Decisions are in the spec's Implementation decisions under the B4a heading; findings left for later are in [carry-over.md](../carry-over.md).

## Comments

### 2026-10-17 resolved

Merged into `feature/logs-v2`; the full suites passed at the end of its batch.
