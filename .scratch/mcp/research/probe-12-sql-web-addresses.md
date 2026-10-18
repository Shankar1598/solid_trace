# Ticket 12 probe: what SQL can reach with httpfs loaded, DuckDB 1.5.6 (duckdb-go v2.10506.0), 2026-10-18

A throwaway Go program on an in-memory DuckDB set up as `UseBucket` does: autoload off, `LOAD httpfs`, an S3 secret scoped to `s3://solidtrace-dev/` on MinIO. A local HTTP listener stood in for an internal service, and counted TCP connections, so a TLS attempt counts too. Each line is a URL read with `read_text`; "hit" means it reached the listener.

**httpfs loaded, nothing else (today's bucket installs):**
- `http://127.0.0.1:PORT/a`: hit, and the body comes back.
- `s3://other/x?s3_endpoint=127.0.0.1:PORT&s3_use_ssl=false&s3_url_style=path`: hit, body back. The same through `s3a://`, `s3n://`, `gcs://`, `gs://` and `r2://`, and even `s3://solidtrace-dev/x?s3_endpoint=…`, inside the secret's scope. S3 URLs take their settings from the query string.

**`SET GLOBAL disabled_filesystems = 'HTTPFileSystem'`** (the issue's option 2):
- `http://` and `https://`: "Permission Error: File system HTTPFileSystem has been disabled by configuration". The bucket still reads.
- Every `?s3_endpoint=` URL above: still a hit. So option 2 alone doesn't fix it.

**And `SET GLOBAL s3_url_compatibility_mode = true`** (no globs or query strings in S3 URLs):
- The `?s3_endpoint=` URLs: no hit. The query string becomes part of the key (`https://other.s3.amazonaws.com/x%3Fs3_endpoint%3D…`).
- `s3://solidtrace-dev/probe12/*.parquet`: 404, globs are off. A list of files still reads.
- But the bucket name is the host in virtual-hosted URLs: `s3://127.0.0.1:PORT#/x` is sent to `https://127.0.0.1:PORT#.s3.amazonaws.com/x`, which connects to 127.0.0.1:PORT. Hit, over TLS. The same with `?` for `#`, `a@127.0.0.1:PORT#`, and through `s3a`, `s3n` and `r2`. So any internal HTTPS service and any port can still be reached.

**And `SET GLOBAL s3_url_style = 'path'`:**
- Every URL above goes to `https://s3.amazonaws.com/<bucket>/<key>`, with the bucket percent-encoded (`127.0.0.1%3APORT%23`). No hit for any of them. `gcs://` and `gs://` were already path-style on `https://storage.googleapis.com/`, and `r2://` with no account goes to S3's host.
- The bucket's own `s3://` URLs and a list of them read on MinIO, on every pooled connection.
- A secret with no `URL_STYLE` follows the global setting: an AWS bucket's secret would move from `https://acme-aws2.s3.eu-west-1.amazonaws.com/x` to `https://s3.eu-west-1.amazonaws.com/acme-aws2/x`. A secret with `URL_STYLE 'vhost'` keeps `https://acme-aws.s3.eu-west-1.amazonaws.com/x`.

**Other notes:**
- `hf://` reaches `https://huggingface.co/…`. With `HuggingFaceFileSystem` disabled, it gives the same Permission Error.
- `GCSFileSystem` and `R2FileSystem` aren't file system names here. `disabled_filesystems` accepts any name without complaint, so it can't tell you a name is wrong. `gcs://` and `r2://` are S3 under other names.
- A disabled file system can't be turned on again, even by `SET` ("has been disabled previously, it cannot be re-enabled").
- The settings are global: a connection opened after them is refused the same way. `current_setting('disabled_filesystems')` reads as empty on it, so check the behaviour, not the setting.

**Left:** SQL can still send GET and HEAD requests to S3's and GCS's public hosts, with any bucket and key. That can read public buckets, or carry data in the path of a request to someone's bucket. Those are public hosts, not internal ones, and an agent can send what it read anywhere with its own tools anyway.
