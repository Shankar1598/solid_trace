# Log files and the Event archive in a bucket

EventStore can keep its Log files and its Event archive in one S3 or GCS bucket, or in any store with S3's API, such as MinIO or Cloudflare R2. Without a bucket, everything stays on local disk. A bucket costs far less than disk for data that is read rarely, such as last month's Logs. [ADR 0008](../adr/0008-log-parquet-in-a-bucket.md) says why it works this way.

## What goes in the bucket

- **Log files.** Each hour of a Project's Logs is written to a Parquet file by the hourly export. EventStore uploads each file as soon as the export publishes it, to `<prefix>project=<id>/day=<date>/hour=<hour>/<file>.parquet`. The local copy stays for `local_cache_hours` after the file's hour ends. After that, DuckDB reads the file from the bucket, in place.
- **The Event archive.** Each archive run writes a Parquet file for each day it archives. EventStore uploads each file after the run, to `<prefix>events/event_date=<date>/data-<run>.parquet`, and then deletes the local copy. Archived Events are always read from the bucket.

Everything else stays on local disk: the Logs not yet exported, the Events not yet archived, Pebble and the databases.

## The config

Add an `object_store` block to `event_store.yml`:

```yaml
object_store:
  kind: gcs
  bucket: acme-solidtrace
  prefix: solidtrace/
  local_cache_hours: 24
```

| Key | Default | What it does |
| --- | --- | --- |
| `kind` | none, required | `s3` or `gcs`. `gcs` uses GCS's S3 interop API with an HMAC key. |
| `bucket` | none, required | The bucket's name. |
| `prefix` | `solidtrace/` | Goes before every key, so the bucket can hold other things too. A `/` is added at the end if it's missing. Set it to `""` to use the whole bucket. |
| `region` | `auto` for `gcs`; `us-east-1` with an `endpoint` | The bucket's region. It is required for AWS S3. |
| `endpoint` | AWS's own for `s3`; `https://storage.googleapis.com` for `gcs` | The URL of the S3 API, for MinIO, R2 and other stores. When it is set, requests use path-style URLs. |
| `local_cache_hours` | `24` | How many hours after a Log file's hour ends its local copy is kept. `0` deletes the copy once it is uploaded. It doesn't apply to the Event archive. |

- The bucket's key comes from the environment, never from the file: `SOLIDTRACE_OBJECT_STORE_KEY_ID` and `SOLIDTRACE_OBJECT_STORE_SECRET`. `deploy/docker-compose.yml` passes both through to EventStore. EventStore takes the key only from these, not from an instance role or `~/.aws`.
- EventStore doesn't start if the block is wrong: a misspelt key, an empty block, an unknown `kind`, a missing `bucket`, or a missing key in the environment. To keep everything on local disk, remove the block.

Other settings that matter with a bucket:

- `duckdb_extension_directory` (default `/usr/local/lib/solidtrace/duckdb_extensions`) is where DuckDB's httpfs extension is, as `<version>/<platform>/httpfs.duckdb_extension`. The Docker image puts it there. Outside Docker, see [MinIO for development](#minio-for-development).
- `logs_bin_cache_mb`: see [The bin cache](#the-bin-cache).
- `logs_disk_limit_gb`: see [`local_cache_hours`](#local_cache_hours).

## `local_cache_hours`

Every Cleanup pass deletes the local copies of Log files that are uploaded and whose hour ended `local_cache_hours` ago or more. It never deletes a copy that a read is using. A read uses a file's local copy while there is one, and otherwise reads the file from the bucket.

- A higher value keeps recent charts fast and makes fewer requests to the bucket, but uses more disk.
- `0` reads every exported hour from the bucket right after its upload.
- A lower value takes effect at the next Cleanup. A higher value doesn't bring back copies already deleted.
- A Log fetched by its id without its time is looked for in local files only. Otherwise every file in the bucket would be read.

The disk limit (`logs_disk_limit_gb`) comes before `local_cache_hours`. When the Log files take more disk than the limit, EventStore first deletes uploaded local copies, oldest first. It deletes Logs only if no uploaded copy is left, which means the bucket has been failing, and it logs an error when it does.

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

1. Run MinIO on `127.0.0.1:9000` and create a bucket, for example with `mc mb local/solidtrace-dev`.
2. Download httpfs for the DuckDB version EventStore uses (`DUCKDB_VERSION` in `event_store/Dockerfile`) and your platform:

   ```sh
   dir=$HOME/.cache/solid-trace/duckdb_extensions/v1.5.6/linux_amd64
   mkdir -p "$dir"
   curl -fsSL https://extensions.duckdb.org/v1.5.6/linux_amd64/httpfs.duckdb_extension.gz | gunzip > "$dir/httpfs.duckdb_extension"
   ```

3. Configure the bucket and the extension directory. The region defaults to `us-east-1` when an `endpoint` is set:

   ```yaml
   object_store:
     kind: s3
     bucket: solidtrace-dev
     prefix: dev/
     endpoint: http://127.0.0.1:9000
     local_cache_hours: 0
   duckdb_extension_directory: /home/you/.cache/solid-trace/duckdb_extensions
   ```

   With `local_cache_hours: 0`, every exported hour is read from MinIO right after its upload.

4. Start EventStore with MinIO's user and password, or a MinIO access key, in `SOLIDTRACE_OBJECT_STORE_KEY_ID` and `SOLIDTRACE_OBJECT_STORE_SECRET`.

The Go tests that run against a real S3 API skip unless `SOLIDTRACE_TEST_S3_ENDPOINT` is set. To run them against MinIO, run this from `event_store/`, with the key in the same two variables:

```sh
SOLIDTRACE_TEST_S3_ENDPOINT=http://127.0.0.1:9000 SOLIDTRACE_TEST_S3_BUCKET=solidtrace-test \
  go test -p 1 -count=1 -run 'MinIO|S3Bucket' . ./storage/
```

## The bin cache

`logs_bin_cache_mb` (default `64`, `0` turns it off) is the memory, in MiB, that keeps charts' time bins of exported hours. When a chart runs again, for example on a Dashboard that refreshes, it reads only the bins it doesn't have yet and the hours not yet exported. It works with or without a bucket, and saves the most with one, because a cached hour sends no requests to the bucket. Late Logs and Retention drop the bins of the hours they change. A Query with `ago()` or `now()` in its text never uses the cache, so pick the time range in the explorer's bar instead.

## DuckDB with a bucket

While a bucket is configured, EventStore:

- loads httpfs from `duckdb_extension_directory`; DuckDB's extension autoinstall and autoload are off on every install, so DuckDB never downloads code;
- creates one secret for the bucket, held in memory, which every read uses, SQL queries too;
- turns on DuckDB's Parquet metadata cache, HTTP metadata cache and external file cache (`parquet_metadata_cache`, `enable_http_metadata_cache` and `enable_external_file_cache`). They keep files' metadata and recently read data in memory, within `duckdb_memory_limit`, so reading the same file again sends few requests or none;
- gives DuckDB 4 threads for each CPU, because httpfs holds a thread for each request it waits on;
- keeps httpfs to S3 URLs, so SQL queries reach no web address but the bucket's and S3's and Google Cloud Storage's public hosts ([ADR 0003](../adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md)). It turns DuckDB's HTTP and Hugging Face file systems off, and an S3 URL takes no settings from its query string and no globs (`s3_url_compatibility_mode`). An S3 URL outside the bucket is path-style (`s3_url_style = 'path'`), while an AWS bucket's own URLs stay virtual-hosted.

The HTTP metadata cache is never cleared while EventStore runs, so never write an object under the prefix again with different bytes.

## When the bucket is down

- Ingest and processing carry on.
- Uploads are tried again every minute. Until a file is uploaded, its local copy stays and reads use it.
- Reads of Log hours whose only copy is in the bucket fail.
- Starting EventStore doesn't wait for the bucket. Event reads answer 503 until EventStore has listed the Event archive in the bucket, which it tries every minute. They also answer 503 if the events view can't be rebuilt after an archive run, until a retry rebuilds it, rather than answer without that run's Events.

## Changing the block later

- **Adding a bucket** to an install that already has Logs and Events: at its next start, EventStore uploads every Log file and Event archive file it has. Local copies then go as described above.
- **Removing the block** takes EventStore back to local disk only. Files whose only copy is in the bucket are no longer read: reads of those Log hours fail, and Event reads leave out those archived Events. EventStore warns at every start how many such files there are. Add the block back to read them again.
- **Changing the bucket or the prefix:** EventStore reads each file it uploaded at the same key in the bucket and prefix it is configured with. Copy every object to the new place first, keeping its key below the prefix.
- **Don't delete or change objects under the prefix** yourself or with lifecycle rules. EventStore still lists the Log files, so reads of their hours would fail. Retention and Project deletion delete Log files' objects for you. Nothing deletes archived Events yet, on disk or in the bucket.
