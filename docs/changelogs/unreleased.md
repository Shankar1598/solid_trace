# Unreleased

The notes of the next release, for the changes since v0.24.2 on this file. At the tag, rename this file to `<tag>.md`, add the notes of the other changes, and add its Release record.

## Added

- **Evidence files: the screenshots and videos that prove a PR.** `open-pr` and `update-pr` upload the screenshots and videos of their checks to SolidTrace, and the Evidence section of the PR body has one link for each file, as **Screenshot: <caption>** or **Video: <caption>**. A reviewer on GitHub or Bitbucket clicks the link and sees the file in Console after sign-in. The files are not public, and the forge token needs no write scope. The agent decides which files are safe to upload. When an upload fails, the skill writes no link for the file, and its final reply says why. See [Prove a PR with screenshots and videos](../agent-sessions.md#prove-a-pr-with-screenshots-and-videos-evidence-files) and [ADR 0025](../adr/0025-evidence-files-are-kept-by-eventstore-and-shown-by-console.md).
  - `solidtrace evidence add FILE... [--caption TEXT] [--session ID]` uploads each file to the Agent session of `--session`, else of `CLAUDE_CODE_SESSION_ID`, and prints the link, the type and the size of each file. It takes PNG, JPEG, WebP and GIF images, and MP4 and WebM videos, of at most 32 MiB, found from the bytes. The same file again to the same Agent session gives the same link.
  - EventStore keeps the bytes in the bucket of `object_store`, under the `evidence/` prefix, else in `evidence_path` on its data disk.
  - `/<org>/evidence/<id>` shows the image, or plays the video with the speeds 0.25x, 0.5x, 1x and 2x, with the caption, the uploader, the date and links to the Agent session and its Review record. Only the members of the Organization can open it. **Coding agents** → **Evidence files** lists the files, the Agent session page lists the files of that Agent session, and the Review record page lists the files of the Agent sessions of all the rounds of its PR.
- **Evidence Retention.** An Admin sets how many days the Agent Project keeps each Evidence file, on **Settings** → **Agent sessions**, or with `update_project`'s `evidence_retention_days`. The default is 90, from 1 to 3650. EventStore deletes the older files once a day, and the link then says when and why.

## Changed

- **The Coding agents group in the sidebar.** One **Coding agents** group holds **Agent sessions**, **Review records** and **Evidence files**. It opens on the page of each of them, and a click on its heading opens or closes it. The Agent sessions and Review records tabs are gone. **Runs** stays its own entry.

## Upgrading

Upgrade EventStore and Console before the plugin. The new `open-pr` and `update-pr` skills call `solidtrace evidence add`, and an older EventStore answers it with 404. The skills then write no Evidence links.

1. Add the line `evidence_path: /storage/evidence/production` to `deploy/config/event_store.yml`. `deploy/install` keeps the config of an earlier install, which has no `evidence_path`, and EventStore then uses its default folder, `/storage/evidence/development`. A new install gets the line from `deploy/event_store.yml`.
   If you upgraded with no `evidence_path` and EventStore has Evidence files with no bucket, keep the default, or move the files before you add the line. Without the move, Console cannot show the older files, and Evidence Retention does not delete them. To move them:
   1. Stop EventStore: `sudo docker compose -f deploy/docker-compose.yml stop event_store`.
   2. Move the folder `deploy/data/storage/evidence/development/evidence` to `deploy/data/storage/evidence/production/evidence` (in the container, `/storage/evidence/development/evidence` to `/storage/evidence/production/evidence`).
   3. Add the line, and start EventStore again: `sudo docker compose -f deploy/docker-compose.yml up -d event_store`.
2. Then upgrade the install. Console runs its two new migrations (`evidence_files`, and `projects.evidence_retention_days`) when it starts.
3. With no bucket, EventStore keeps the files in `evidence_path`, on the data disk, and only Evidence Retention limits them: `logs_disk_limit_gb` does not count them. With a bucket, they go under its `evidence/` prefix, no new bucket is needed, and `evidence_path` keeps only the temporary files of uploads.
4. Optional: `deploy/docker-compose.yml` gives EventStore `CONSOLE_URL`, for the links when the Console URL instance setting is not set.
5. Then each engineer runs the setup wizard again, which updates the plugins and the CLI.
