# Logs in a bucket

EventStore keeps Logs and Events in ClickHouse, on local disk. With a bucket, ClickHouse moves older Logs to it: one S3 or GCS bucket, or any store with S3's API, such as MinIO or Cloudflare R2. Without one, everything stays on local disk. A bucket costs far less than disk for data that is read rarely, such as last month's Logs. [ADR 0016](../adr/0016-logs-and-events-in-clickhouse.md) says why it works this way.

## What goes in the bucket

- **Logs older than the hours on local disk.** ClickHouse keeps Logs in parts, each of one Project's UTC day. A part moves to the bucket once its newest Log is as many hours old as **Hours on local disk** on Console's Settings → Instance says (`local_cache_hours`, 24 by default; see [Changing the hours on local disk](#changing-the-hours-on-local-disk)). A Log that old when it is stored goes straight to the bucket. The bucket holds them under `<prefix>clickhouse/`, in ClickHouse's own layout, and ClickHouse reads them in place, with a read cache of up to 10 GiB on local disk.
- **Log files from before the switch to ClickHouse.** The Log files written before the switch stay where they were, under `<prefix>logs/`, and are read in place until Retention deletes them. Nothing new is written there.

Everything else stays on local disk: the histogram's counts, the Fields' counts, Events, Pebble and the databases. ClickHouse keeps on local disk the list of the parts it moved, so a backup or a disk snapshot must include ClickHouse's data directory: the bucket alone can't be restored from.

## The config

Add an `object_store` block to `event_store.yml`:

```yaml
object_store:
  kind: gcs
  bucket: acme-solidtrace
  prefix: solidtrace/
```

| Key | Default | What it does |
| --- | --- | --- |
| `kind` | none, required | `s3` or `gcs`. `gcs` uses GCS's S3 interop API with an HMAC key. |
| `bucket` | none, required | The bucket's name. |
| `prefix` | `solidtrace/` | Goes before every key, so the bucket can hold other things too. A `/` is added at the end if it's missing. Set it to `""` to use the whole bucket. |
| `region` | `auto` for `gcs`; `us-east-1` with an `endpoint` | The bucket's region. It is required for AWS S3. |
| `endpoint` | AWS's own for `s3`; `https://storage.googleapis.com` for `gcs` | The URL of the S3 API, for MinIO, R2 and other stores. When it is set, requests use path-style URLs. |

How long Logs stay on local disk isn't in the file: an Instance admin sets it on Settings → Instance. A file that still sets `local_cache_hours`, which v0.18.0 and earlier read, loads with a warning naming Settings → Instance, and the value is ignored.

- The bucket's key comes from the environment, never from the file: `SOLIDTRACE_OBJECT_STORE_KEY_ID` and `SOLIDTRACE_OBJECT_STORE_SECRET`. `deploy/docker-compose.yml` passes both to EventStore and to ClickHouse. EventStore takes the key only from these, not from an instance role or `~/.aws`.
- EventStore doesn't start if the block is wrong: a misspelt key, an empty block, an unknown `kind`, a missing `bucket`, or a missing key in the environment. To keep everything on local disk, remove the block.
- At its start, EventStore writes the bucket into ClickHouse's storage policy, in `clickhouse.config_dir` (ClickHouse's `conf.d`), which ClickHouse reloads by itself.

Other settings that matter with a bucket:

- `logs_bin_cache_mb`: see [The bin cache](#the-bin-cache).
- `logs_disk_limit_gb`: see [The disk limit](#the-disk-limit).

## The disk limit

`logs_disk_limit_gb` caps how much of ClickHouse's local disk every Project's Logs take together; by default, half of that disk. EventStore checks it a few minutes past each hour. Past it, with a bucket, it moves the oldest days to the bucket first, and deletes a day only if that isn't enough, which means the bucket is failing; it logs an error when it does. Without a bucket, it deletes the oldest days, of any Project. ClickHouse also moves the largest parts to the bucket by itself once its local disk is 90% full.

## GCS

GCS is reached through its S3 interop API with an HMAC key for a service account. Replace `acme-solidtrace` and `PROJECT_ID` below with your own.

1. Create the bucket:

   ```sh
   gcloud storage buckets create gs://acme-solidtrace --location=us-central1 --uniform-bucket-level-access
   ```

2. Create a service account for EventStore:

   ```sh
   gcloud iam service-accounts create solidtrace-event-store --display-name="SolidTrace EventStore"
   ```

3. Let it read, write, list and delete the bucket's objects:

   ```sh
   gcloud storage buckets add-iam-policy-binding gs://acme-solidtrace \
     --member=serviceAccount:solidtrace-event-store@PROJECT_ID.iam.gserviceaccount.com \
     --role=roles/storage.objectUser
   ```

4. Create an HMAC key for the service account:

   ```sh
   gcloud storage hmac create solidtrace-event-store@PROJECT_ID.iam.gserviceaccount.com
   ```

   It prints an access ID and a secret. The secret is shown only once. You can also create the key in the Cloud console, under Cloud Storage, Settings, Interoperability: "Create a key for a service account".

5. Set `SOLIDTRACE_OBJECT_STORE_KEY_ID` to the access ID and `SOLIDTRACE_OBJECT_STORE_SECRET` to the secret.

6. Configure the bucket:

   ```yaml
   object_store:
     kind: gcs
     bucket: acme-solidtrace
   ```

   `endpoint` defaults to GCS's interop endpoint, `https://storage.googleapis.com`, and `region` defaults to `auto`.

To rotate the key, create a new one, restart EventStore with it, then deactivate and delete the old key with `gcloud storage hmac update <access id> --deactivate` and `gcloud storage hmac delete <access id>`.

## S3

1. Create the bucket, with Block Public Access on (the default).
2. Create an IAM user for EventStore, with no console access, and attach this policy. Replace `acme-solidtrace` with your bucket and `solidtrace/` with your prefix:

   ```json
   {
     "Version": "2012-10-17",
     "Statement": [
       {
         "Effect": "Allow",
         "Action": "s3:ListBucket",
         "Resource": "arn:aws:s3:::acme-solidtrace",
         "Condition": { "StringLike": { "s3:prefix": "solidtrace/*" } }
       },
       {
         "Effect": "Allow",
         "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
         "Resource": "arn:aws:s3:::acme-solidtrace/solidtrace/*"
       }
     ]
   }
   ```

   With `prefix: ""`, drop the `Condition` and use `arn:aws:s3:::acme-solidtrace/*` for the objects. If the bucket is encrypted with a KMS key of your own, also allow `kms:Decrypt` and `kms:GenerateDataKey` on that key.

3. Create an access key for the user. Set `SOLIDTRACE_OBJECT_STORE_KEY_ID` to its ID and `SOLIDTRACE_OBJECT_STORE_SECRET` to its secret.
4. Configure the bucket, with its region:

   ```yaml
   object_store:
     kind: s3
     bucket: acme-solidtrace
     region: eu-west-1
   ```

Other stores with S3's API, such as Cloudflare R2, also use `kind: s3`, with their `endpoint`.

## MinIO for development

The dev ClickHouse and MinIO (`docker-compose.dev.yml`, started by `bin/clickhouse-dev`) are shared by every checkout, worktree and Go test package, so a bucket on them reaches all of them. MinIO listens on `127.0.0.1:9000` (console on `:9001`) with the key `solidtrace` / `solidtrace-dev`, keeps its data in the main checkout's `storage/minio/`, and makes the buckets `solidtrace-dev` and `solidtrace-test` when they're missing. The dev ClickHouse has that key in `SOLIDTRACE_OBJECT_STORE_KEY_ID` and `SOLIDTRACE_OBJECT_STORE_SECRET` unless they were set otherwise when it started.

1. Configure the bucket. The region defaults to `us-east-1` when an `endpoint` is set:

   ```yaml
   object_store:
     kind: s3
     bucket: solidtrace-dev
     prefix: dev/
     endpoint: http://127.0.0.1:9000
   ```

   With **Hours on local disk** at 0 on Settings → Instance, every part moves to MinIO once it is stored.

2. Start EventStore with the same key in the same two variables.

ClickHouse can't drop a volume while it runs, so taking the block out again needs a restart of the dev ClickHouse too.

The Go tests that need an S3 API make a storage policy of their own (`clickhousetest.Bucket`), never the shared one. They use `SOLIDTRACE_TEST_S3_ENDPOINT`, with the key in the same two variables, or else the dev MinIO with its development key, and skip without either. The bucket is `SOLIDTRACE_TEST_S3_BUCKET`, or `solidtrace-test`. From `event_store/`:

```sh
go test -p 1 -count=1 -run 'Bucket|MinIO|OldLogFiles' . ./storage/
```

## The bin cache

`logs_bin_cache_mb` (default `64`, `0` turns it off) is the memory, in MiB, that keeps charts' time bins of hours that have ended. When a chart runs again, for example on a Dashboard that refreshes, it reads only the bins it doesn't have yet. It works with or without a bucket, and saves the most with one, because a cached bin reads nothing from the bucket. A Log stored into an hour, and Retention or Project deletion of its day, drop that hour's bins. A Query with `ago()` or `now()` in its text never uses the cache, so pick the time range in the explorer's bar instead.

## When the bucket is down

- Ingest carries on: a `200` still means the Logs or Event are in Pebble.
- Reads of Logs in the bucket fail: Logs older than the hours on local disk, and the old Log files whose only copy is there. Recent Logs, Events, the histogram's counts and the Fields still answer.
- Parts wait on local disk to move. If they pass the disk limit, EventStore deletes the oldest days and logs an error.

## Changing the block later

- **Adding a bucket** to an install that already has Logs: at its next start, EventStore adds the bucket to ClickHouse's storage policy, and parts older than the hours on local disk move to it.
- **Removing the block** stops parts moving. The Logs already moved stay in the bucket, and ClickHouse needs the bucket to read them, so keep the block while any are there. ClickHouse can't drop the bucket's volume while it runs, and logs an error until it restarts. Old Log files whose only copy is in the bucket aren't read any more; EventStore warns at every start how many there are.
- **Changing the bucket or the prefix** would lose what ClickHouse moved there. Don't.
- **Don't delete or change objects under the prefix** yourself or with lifecycle rules. ClickHouse's parts would be lost, and EventStore still lists the old Log files. Retention and Project deletion delete Logs, in ClickHouse and in old Log files, for you.

## Changing the hours on local disk

**Hours on local disk** on Settings → Instance, or `local_cache_hours` over the MCP server's `update_instance_settings`, is 0 to 720 hours; 0 moves each part once it is stored. It does nothing without a bucket. EventStore reads it from Console at start and every minute, and when it changed sets the TTL of the `logs` and `spans` tables (`ALTER TABLE … MODIFY TTL`), so a change applies within a minute or two, with no restart. Settings → Instance asks before it saves a change:

- Logs older than the new hours are read from the bucket, which is slower than local disk. Lowering it moves more of the recent Logs there within a minute or two.
- Raising it keeps more on local disk, which counts toward the disk limit. Parts already in the bucket stay there.
- ClickHouse then recomputes every part's move time in the background, reading each part's time column, from the bucket too, so change it rarely. On the dev MinIO, for a table of 5 parts, 223 MiB, all in the bucket, the ALTER returned in 0.37 s, and the background mutation read 3.5 MB (the `time` column) in 12 GETs and wrote about 3.5 MB of part metadata; no data column was read.

If EventStore can't read the setting, or it's out of range, the tables keep the hours they had, and EventStore logs it each minute.

