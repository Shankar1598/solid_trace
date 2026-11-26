# Evidence files are kept by EventStore and shown by Console

The `open-pr` and `update-pr` skills prove a PR with screenshots and videos of their checks. A reviewer on GitHub or Bitbucket must see them, but the files are not public, and the forge token of the skills gets no write scope. So the PR body does not show the files. It links to each **Evidence file**, and the reviewer opens the link in Console after sign-in:

- **EventStore keeps the bytes.** `solidtrace evidence add` sends each file to `POST /api/agent/v1/agent-sessions/:session_id/evidence`, on the agent ingest API, with the `sessions:write` scope. With `object_store` set, the bytes go to the bucket under the `evidence/` prefix. Without it, they go to `evidence_path` on EventStore's data disk.
- **One row in Console's SQLite.** EventStore writes the `evidence_files` row: the Organization, the **Agent session**, the sha256, the caption, the type, the size, the storage key and the uploader. Console's migration makes the table, and the EventStore code that writes it is in the same commit ([ADR 0001](0001-eventstore-writes-console-tables.md)). The row has no foreign key to the Agent session, because a file can arrive before its Agent session.
- **The type from the bytes.** EventStore takes only PNG, JPEG, WebP, GIF, MP4 and WebM, which it finds from the first bytes, not from the file name. It refuses each other type with 415, because a file that Console serves from its own domain, such as SVG or HTML, could run script there. A file is at most 32 MiB, which is in the ingest app's body limit (`agentBodyLimit`, 33 MiB).
- **The same file twice is one row.** A second upload of the same bytes to the same Agent session gives 200 and the first row, and writes no second object.
- **Console shows the file.** The link is `<Console URL>/<org>/evidence/<id>`, with a random id. Console checks that the viewer is a member of the Organization, then streams the bytes from EventStore's query API (`GET /evidence/:id/content`) with `Range` passed through, so a video can seek. It sends the stored `Content-Type`, `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox`, `Content-Disposition: inline` and `Cache-Control: private`. A person who is not signed in signs in and comes back to the link. A person who is not a member gets 404.
- **Evidence Retention.** EventStore deletes each file after the **Agent Project**'s **Evidence Retention** (90 days by default, 1 to 3650): first the object, then it sets `deleted_at`. The row stays, so the link says when the file was deleted and why.

We chose this because the upload path was already there. The CLI sends everything to the agent ingest API, which EventStore serves with its auth and its scope check. EventStore already has the bucket client on the `object_store` config of the Log Parquet ([ADR 0008](0008-log-parquet-in-a-bucket.md)), so a production install needs no new bucket. Console already reads the Agent sessions and the Review records from EventStore's query API, and the Evidence files use the same path.

## Considered options

- **Console keeps the files with Active Storage.** This was the first draft of the spec. The CLI would need a second upload target with its own auth, and Console would need a second bucket config next to EventStore's.
- **Images in the PR body.** GitHub loads each Markdown image through its own image proxy, which has no Console Session, so a private image shows as broken. A browser does not send Console's Session cookie (`SameSite=Lax`) with an image on a Bitbucket page. A public URL would let each person with the link open the file, and GitHub's proxy keeps a copy.
- **The forge's attachments.** GitHub's and Bitbucket's PR attachments have no public API. Bitbucket's Downloads API needs a write scope on the repository.

## Consequences

- Deploy order: upgrade EventStore and Console before the plugin. An older EventStore answers 404 to the upload. The CLI then says that the server is too old for Evidence files, and the skill writes no link for the file and says why in its final reply.
- The agent decides which files are safe to upload. No person confirms each file, and SolidTrace does not redact an image. Each member of the Organization can see each Evidence file.
- A reviewer who is not a member of the Organization cannot see the files.
- The link names the Console URL instance setting, else EventStore's `CONSOLE_URL`. With neither, EventStore gives a path, and the CLI joins it to the Console URL of its sign-in.
- A ranged read on GCS's S3 API is proven only in production. The tests read a range from MinIO, through the same S3 API.

Source: [.scratch/evidence-files/plan.md](../../.scratch/evidence-files/plan.md)
