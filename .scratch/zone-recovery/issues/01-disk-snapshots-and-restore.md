# 01: Snapshots of the VM's disk, and a restore into another zone

**Status:** ready-for-agent

**What to do:** Give a GCP install a backup of its disk and a tested way to bring it back in another zone. Asked for by the user on 2026-10-20, after a zone failed in London (`europe-west2`). SolidTrace's production VM is in `europe-west4-a` and wasn't affected, but it has no backup at all.

## How production is hosted (2026-10-20)

Read with gcloud as `llm-dev` in `tagalys-v2`.

- One VM, `solid-trace`: `n4a-standard-2` (arm64) in `europe-west4-a`, created 2026-10-19.
- One disk, `solid-trace`: the 80 GB zonal `hyperdisk-balanced` boot disk. It holds everything in `deploy/data/`, `deploy/.env` and `deploy/config/`.
- **No snapshot schedule and no snapshots.**
- Already safe from a zone failure:
  - the static IP `solid-trace`, `35.214.179.92`, which is regional (Standard tier);
  - the bucket `gs://tagalys-solid-trace`, which is regional in `EUROPE-WEST4` and holds about 600 MB of Log files under `solidtrace/logs/`. No `events/` yet.

## Why the bucket isn't a backup

The bucket gets only Log files, each one uploaded once its hour is exported ([ADR 0008](../../../docs/adr/0008-log-parquet-in-a-bucket.md)), and the Event archive, which runs once a day for days before today (`Maintenance::EventStoreArchiveJob`). Only the disk holds:

- Console's SQLite: users, Organizations, Projects, keys, members, Dashboards, Monitors and Issues;
- Events not yet archived, in DuckDB;
- the current hour's Logs, in the hot table;
- `logs.duckdb`'s `log_files` manifest. Reads go only by its rows ([ADR 0007](../../../docs/adr/0007-log-files-listed-in-a-manifest.md)), so without it the Log files in the bucket can't be read;
- the message queue and Pebble;
- `deploy/.env` (`SECRET_KEY_BASE`, the bucket's HMAC key, `META_PROJECT_KEY`), `event_store.yml` and Caddy's certificates.

## Decided with the user (2026-10-20)

- **Snapshots first.** A snapshot schedule copies the whole disk at one moment, so SQLite, DuckDB and the manifest come back consistent with each other. A snapshot is crash-consistent, like a power cut: Pebble, DuckDB and SQLite recover from their WALs. Snapshots are stored outside the zone, and unlike a replicated disk they also cover mistakes and corruption. Recovery loses up to one interval of data and takes about 10 minutes.
- **A regional disk later, maybe.** N4A takes only Hyperdisk, so the regional option is Hyperdisk Balanced High Availability, not regional PD, and N4A supports it. It replicates every write to a second zone, so no data is lost, but a person still force-attaches it to a VM in the other zone. It roughly doubles the disk's cost, every write waits for the second zone, and moving to it means recreating the VM. Only if downtime or the lost interval starts to matter to customers.
- **No automatic failover.** A stateful MIG doesn't do it. Stateful regional MIGs can't move a VM to another zone, because "the preserved state is stored in a specific zone and cannot be moved", and they support only redistribution type `NONE`. The MIG's `failoverAction` accepts only `NO_FAILOVER` ([preserved state](https://docs.cloud.google.com/compute/docs/instance-groups/preserved-state), [configuring stateful MIGs](https://docs.cloud.google.com/compute/docs/instance-groups/configuring-stateful-migs)). Automating failover would need our own watcher, such as Cloud Scheduler and a function that force-attaches an HA disk, which is too much for a rare event. A single-zone stateful MIG would only autoheal a crashed VM in the same zone, which `automaticRestart` mostly covers already.

## What to build

- [ ] Settle with the user, at the start:
  - the interval: hourly, or every 4 hours;
  - how long to keep snapshots: 7 days suggested;
  - where to store them: `europe-west4` or the `eu` multi-region.
- [ ] `deploy/setup-gcp`: a stage that prints the Cloud Shell commands to make a snapshot schedule in the VM's region and attach it to the VM's disk, in the wizard's style. It skips them if one is attached already.
- [ ] A restore for a GCP install, as a wizard (`deploy/restore-gcp`) or a runbook in `docs/`, in another zone of the same region:
  - make a disk from the newest snapshot;
  - make a VM on it with the same machine type;
  - move the static IP to it;
  - check Console signs in, and that Issues, Logs (old hours from the bucket included) and Dashboards are there.

  The certificate is for the IP and comes back with the disk.
- [ ] With the user's go-ahead (it changes production): make the schedule on `solid-trace` and take the first snapshot.
- [ ] Practise the restore once into a throwaway VM, without the static IP, then delete it. Record the time it took and anything the databases logged while recovering in the Comments.

## Comments

- 2026-10-23, from the ClickHouse effort ([spec](../../clickhouse/spec.md)): **to fix for HA, left for now by the user.** After the switch to ClickHouse, Logs older than a day sit in the bucket as ClickHouse chunks whose file map lives only on the VM's disk, so the bucket can't be restored from (today's Log files are at least self-contained Parquet). A restored snapshot can also point at chunks ClickHouse deleted after it (merges, late Logs, Retention), so the restore needs GCS soft delete (check it's on for `gs://tagalys-solid-trace`) and a step that restores objects under `clickhouse/` deleted after the snapshot. The fix discussed: ClickHouse `BACKUP` of `logs`, `events` and the counts tables to `backups/clickhouse/` (weekly full, hourly or daily incremental, two weeks kept; about one more copy of the Logs, ~$7–15/month), Console's SQLite to `backups/console/`, and a restore from the bucket alone. Probe first: `BACKUP` to GCS with explicit keys (26.7+), server-side copy on GCS, `RESTORE` on an empty server.

