# Project status

Updated: 2026-10-20

SolidTrace: self-hosted error tracking with two services, Console (Rails) and EventStore (Go), and embedded databases. Terms are in [CONTEXT.md](../CONTEXT.md).

**Stage:** 0.1 prototype, not a production release. The goal is to build it and see the end result. Production hardening is deferred until after the prototype.

**Rules for agents:**
- Don't pick up deferred hardening unprompted: the open items in [prod-readiness review.md](prod-readiness/review.md) (§1, §9, §10: tiered storage, retention, `mode=ro`, the `ValidateKey` cache, compose host networking, DSN, ingest limits, metrics, CI), 011 and 012. Each is marked deferred or `wontfix` there. A change that would make one of them worse is still worth a flag.
- EventStore runs DuckDB 1.5.6, bundled with the driver (upgrade-pebble-duckdb 02, 2026-10-02). There is no alpha library or `duckdb_use_lib` tag any more. A shell opened before that change may still export `GOFLAGS=-tags=duckdb_use_lib`; open a new one.
- Everything is merged into `main` (2026-10-18), and the main checkout is on `main`, with no worktrees left. The dev stack runs from the main checkout, on the dev data it used from the `main` worktree. The main checkout's older `feature/logs-v2` dev data is in `~/.cache/solid-trace/storage-feature-logs-v2-2026-10-18/`. The feature branches are all merged and kept.
- Read [ADR 0001](../docs/adr/0001-eventstore-writes-console-tables.md) before touching `event_store/storage/sqlite.go` or a Console enum or table that EventStore writes. A change to one must change the other in the same commit.
- The logs map is the source of truth for logs; trust it over the counts below.
- Benchmark and probe runs must finish in under a minute on the dev box.

## Where things stand

- **Org selector** ([spec](org-selector/spec.md)): planned with the user on 2026-10-19, after they couldn't reach the Meta Organization's Logs on their VM install. With two or more Organizations, the sidebar header is a dropdown of them, by name; switching opens the same section's index there, and the palette gains an Organizations group. `/` opens the last used Organization, kept in the session cookie, with the first by name as the fallback. The Organization lookup copied across 13 controllers becomes one concern. Built on `feature/org-selector` and merged into `main` (2026-10-20); [tickets](org-selector/issues/) 01 to 03 resolved. The last used Organization is recorded only on page visits (GET, Inertia or HTML), not on JSON polls; names sort case-insensitively (`User#organizations_by_name`). Reviewed (standards and spec): nothing high, fixes merged. Left as is: the palette's Organizations go to Issues, as the spec says, while the sidebar keeps the section. Targeted Rails tests, vitest, typecheck and the switch-organization, palette and make-organization flows pass; full suites are for the user. Not released yet: the user wants the Project key reachable again after Logs arrive (the Logs Get started page disappears) before the next version. Also on 2026-10-19: `db:seed` makes its development users and Organization only in development and test (`a4da4c6`), after they turned up on the user's production install.
- **Request rates on Charts** ([spec](request-rates/spec.md), [tickets](request-rates/issues/)): planned with the user on 2026-10-19, after Better Stack's 15-second buckets overstated a White Fox peak. Built on `feature/request-rates` and merged into `main` (2026-10-19, `c86ee5d`), released in v0.4.0. Both tickets are resolved. [01](request-rates/issues/01-chart-shows-its-bin.md): every time Chart says its bin size, such as `1m bins`, and its tooltip gives the bin's span. [02](request-rates/issues/02-rate-and-peak-rate-aggregates.md): the `rate()` and `peak_rate()` aggregates, in one summarize the bin cache still takes. `bin_auto` stays at about 100 bins. Over 7 days of dev Logs (1.86 M), `rate(), peak_rate() by bin_auto(time)` answers in 0.5–0.6 s cold. Reviewed (standards and spec): nothing high, fixes merged. Left as is: one shared table of aggregate traits instead of parallel lists in Go and TS. Targeted tests and the Charts, Builder, Dashboards and Monitors flows pass; the full Go, Rails and Playwright suites are for the user.
- **SolidTrace's own Logs** ([spec](self-logs/spec.md), [tickets](self-logs/issues/)): planned with the user on 2026-10-19. A VM install seeds a Meta Organization with a "SolidTrace" Project, and Vector ships every container's output from Docker to it, through a new key-only route, `POST /api/logs` ([ADR 0011](../docs/adr/0011-meta-organization-and-vector-reads-docker-logs.md)). Built on `feature/self-logs` and merged into `main` (2026-10-19, `672b95a`), released in v0.2.0: 01 and 02 resolved, 03 `ready-for-human` (Vector checked end to end on the dev box against a stand-in EventStore; the real VM run remains). Image `timberio/vector:0.58.0-alpine`; the key is `META_PROJECT_KEY` in `deploy/.env`, and Vector reads it only with `VECTOR_DANGEROUSLY_ALLOW_ENV_VAR_INTERPOLATION=true`. Reviewed (standards and spec), fixes merged. Left open: the first-sign-up admin rule runs only in sign-up and isn't race-safe; Rails backtrace lines become level-less Logs. Targeted tests pass; full Go and Rails suites are for the user. Also on 2026-10-19: EventStore logs each file it uploads to the bucket and each local copy it deletes (`8b30ab6`), and the message queue's rows are deleted after 30 days (`cc3ce5e`).
- **HTTPS** ([spec](https/spec.md), [01](https/issues/01-caddy-terminates-tls.md)): planned with the user and built on `main` (2026-10-19). With HTTPS on in `deploy/setup-gcp`, Caddy terminates TLS for the VM's bare public IP, with Let's Encrypt's 160-hour certificates: Console on 443, ingest on 4443 ([ADR 0010](../docs/adr/0010-caddy-terminates-tls.md)). Checked with `caddy validate`, `docker compose config` and Console's production config; reviewed. `ready-for-human`: the real certificate needs the user's GCP VM.
- **Merged into `main`** (2026-10-18), each with `--no-ff` and no conflicts: `feature/mcp` (Logs v1 and v2, the looks pass, the MCP server, bugs 01 to 07), then `feature/console-bugs` (bugs 08, 09, 11, 12), then `feature/logs-v2` (`deploy/install`, `deploy/setup` and production email settings). On the merge: typecheck, vitest (399), the 288 Rails tests for the changed code and the 12 affected Playwright flows pass, among them the SQL flow on a rebuilt EventStore. The full Go, Rails and Playwright suites are for the user to run. Performance testing and the real GCS run happen on `main`.

- **MCP server**: planned with the user on 2026-10-17 ([spec](mcp/spec.md), [tickets](mcp/issues/)), on `feature/mcp` in the worktree `../solid_trace-mcp`, branched from `feature/logs-v2` at `4139293`. An MCP endpoint in Console at `POST /mcp`, signed in with API tokens from User settings, with 34 tools for Logs (Queries, SQL, Fields), Dashboards, Monitors, Issues and admin actions. SQL queries are read-only everywhere, and keys are admin-only. Tickets 01 to 10 are resolved: built and reviewed on 2026-10-17, with the Go track merged at `19939fb`; on 2026-10-18 the looks pass was merged in (`d3de5fa`), [docs/mcp.md](../docs/mcp.md) written, all 77 Playwright flows passed on `feature/mcp`, and an end-to-end run with the MCP inspector made a token in User settings, listed the tools and called the Logs, Dashboard and Monitor tools ([10](mcp/issues/10-docs-and-wrap.md)'s Comments). Rails (575 runs), vitest (391), typecheck and the Go suite pass. The dev stack now runs `feature/mcp` from the worktree. The whole-effort review (11) found nothing high or medium in the code; it restored the spec's plan sections, which recording the decisions had overwritten. Lows are in [carry-over.md](mcp/carry-over.md). [12](mcp/issues/12-sql-fetches-web-addresses-on-bucket-installs.md) is resolved (2026-10-18): on bucket installs, read-only SQL could make the server fetch any web address, internal ones included. httpfs now reads only S3 URLs, so SQL reaches the bucket's endpoint and S3's and GCS's public hosts, and nothing else.

- **Bugs** ([issues](bugs/issues/)): seven Console bugs from before the logs and MCP work, found on 2026-10-17 and 18. Five were fixed on `feature/mcp` on 2026-10-18:
  - the profile form, which never saved;
  - password reset, built behind "Forgot your password?";
  - emails, now matched in any case;
  - Project slugs, which the New Project page no longer saves unreachable;
  - a user in no Organization, who now makes one on a page of its own, and sign-up, which had never worked in the browser.

  Their Playwright flows pass. On `feature/console-bugs` (2026-10-18), from the looks review: [08](bugs/issues/08-issues-time-filter-does-nothing.md) the Issues time filter, [09](bugs/issues/09-issues-checkboxes-select-nothing.md) its checkboxes (bulk Resolve and Unresolve), [11](bugs/issues/11-issue-age-counts-from-storing.md) Age from the first Event, and [12](bugs/issues/12-delete-project-does-nothing.md) Delete project, are resolved. [10](bugs/issues/10-issues-users-always-zero.md) resolved: nothing records an Issue's users, so the column is hidden. The user's answers (2026-10-18): the Issues page keeps 14 days and gained "All time"; no bulk assign; EventStore now deletes a deleted Project's Events, at start and daily, archive rows aside ([12](bugs/issues/12-delete-project-does-nothing.md)). Also found: the Issue page's Resolve and Unresolve sent `PUT`, and the routes accept only `PATCH`; fixed on `main` (2026-10-18), with a flow. Open: [05](bugs/issues/05-production-mail-unconfigured.md) (`needs-triage`), production has no mail settings; `deploy/setup` (`feb0cdc`) now reads `CONSOLE_URL`, `MAIL_FROM` and `SMTP_*`, see its Comments. [07](bugs/issues/07-sql-reads-every-organization.md) resolved (2026-10-18): SQL queries read only their own Project's `logs` and `events` views; the read-only check refuses every other table, file, URL, table function and the catalogue. Its updated Playwright flow passes on `main`. After it, SQL queries stop after 10 seconds and are on by default.
- **Logs v2**: implemented on `feature/logs-v2` (2026-10-10 to 17): charts in the explorer, developer UX, Dashboards, Monitors with Slack, PagerDuty and email, Log Parquet and the Event archive in S3 or GCS (tested on MinIO), and a bin cache. 20 of 24 [tickets](logs-v2/issues/) resolved, including a whole-branch review; Rails, vitest, typecheck, all 69 Playwright flows and the Go suite pass. Open: the merge (`ready-for-human`), a real GCS run (`needs-info`), leftover lows (`ready-for-agent`). The looks pass is committed (`82ff11d`).
- **Logs**: v1 is implemented on `feature/logs-v1` (2026-10-03): 19 of 20 [tickets](logs-v1/issues/) resolved, code-reviewed, with the review fixes merged (`0eecf81`). It is not yet merged into `feature/logging` or `main`, and not yet clicked through in a browser. M1-10 (load test) is the only open ticket. Choices that depart from the spec are in its [Implementation decisions](logs-v1/spec.md#implementation-decisions); the review's fixes are in its Comments.
- **Prod-readiness**: closed for 0.1. [review.md](prod-readiness/review.md) items are deferred, except §7, filed as [015](prod-readiness/issues/015-issue-list-counts-and-pagination.md), resolved (2026-10-02). §8 is decided in [ADR 0001](../docs/adr/0001-eventstore-writes-console-tables.md). 011 and 012 are `wontfix` for now. The fixed items (§2, §4, §5, §6, 013, 014) stay fixed.
- **Integration notification**: resolved and merged (2026-10-02, `9a488c6`). One Console module sends new-Issue, threshold and assignment notifications through the outbox, with recorded outcomes, bounded retries and cleanup. EventStore's Event count honours the time window. Review findings left open are listed in the [spec](integration-notification/spec.md)'s Comments.
- **Ingest write path**: reopened. 02 and 03 are resolved (2026-09-30). A code review on 2026-10-01 filed 04–06. 04, a lone bad DuckDB row stalling Event processing, is resolved (2026-10-01). 05 is `wontfix` (2026-10-02). 06 is resolved (2026-10-02). Event ingest stores the Event in Pebble before the `200`. Event processing, a single worker, catches up Issues, DuckDB and the Console from per-Project processing cursors in Pebble. Review §4 is fixed. Accepted gap: a crash can lose an `issue_created`.
- **Upgrade Pebble and DuckDB**: EventStore uses Pebble v2.1.7 and DuckDB 1.5.6 (`v2.10506.0`, bundled). [02](upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md) moved it back from the 2.0 alpha on 2026-10-02, on `feature/logging`, now in `main`. [03](upgrade-pebble-duckdb/issues/03-pebble-value-separation.md) (Pebble value separation) was benchmarked on 2026-10-18 (merged into `main`) and closed as `wontfix` with the user: it stays off. The [research note](upgrade-pebble-duckdb/research/pebble-value-separation.md) has the figures. It saves little on writes, makes cold reads slower and the store bigger, and does nothing on a shared tier.

## Next

- **Log transform** ([spec](log-transform/spec.md), [tickets](log-transform/issues/)): built on `feature/log-transform` (2026-10-21), merged into `main` for v0.7.0. Each Project has one VRL program that Log processing runs on its Logs before DuckDB; admins edit it on the Project's Log transform page (linked from settings) or with MCP `get_log_transform`, `preview_log_transform` and `update_log_transform`. Tickets 01 to 07 resolved: the probe passed at about 9 µs per Log on x86_64 (ADR 0012), the hook and normalisation (02), the image's Rust stage (03), the check/preview/stats routes (04), versions, saving and MCP (05), the page (06) and its preview (07). Reviewed (standards and spec): nothing high; mediums and most lows fixed (`558575c`). Targeted Go root tests, Rails tests, vitest, typecheck and the `log-transform`, `query-editor` and `project-keys` flows pass; full suites are for the user. Adds a migration (`CreateLogTransformVersions`) and Rust to `mise.toml`; every `go build`/`go test` in `event_store/` now needs `event_store/vrl/build` first. Not verified: an arm64 image build and a real release run. Next: [08](log-transform/issues/08-tagalys-sidekiq-program.md) (`ready-for-human`) once v0.7.0 is deployed. Found on the way: `TestLogTimeFormsAndTimesThatBecomeTheReceiptTime` is flaky on `main` (6 of 10 runs failed, "within Retention: not listed").
- **Zone recovery** ([01](zone-recovery/issues/01-disk-snapshots-and-restore.md), `ready-for-agent`): the next item to take up, asked for by the user on 2026-10-20 after a zone failed in London. The production VM (`solid-trace`, `europe-west4-a`) has one zonal disk and no snapshots. The bucket isn't a backup: Console's SQLite, Events not yet archived, the hot Logs and `logs.duckdb`'s manifest are only on the disk. Decided: a snapshot schedule and a tested restore into another zone, through `deploy/setup-gcp`. Not now: a Hyperdisk Balanced HA disk. Not at all: automatic failover, since a stateful MIG can't move a VM across zones. Settle the interval and retention with the user first; creating the schedule on production needs their go-ahead.
- **Invite links** ([spec](invite-links/spec.md), [tickets](invite-links/issues/)): planned with the user on 2026-10-20, after inviting from the Members page failed with a 400 on their mail-less install. The page posts `{ email, name }`, and the controller requires `member`, so every browser invite has failed. Inviting someone new makes them an Invited member (no password yet). Admins copy a one-use, 7-day invite link from the row, and opening it asks for a password and signs them in to that Organization. Mail carries the link only when SMTP is set up, and no password is ever emailed. People who already have a password are added at once, as today. Built on `feature/invite-links` (2026-10-20); [tickets](invite-links/issues/) 01 to 03 resolved, merged into `main` (`9435de6`) and released in v0.6.0. 01 (the 400) is in v0.5.0. 02: `users.invited`, `generates_token_for :invite`, `GET`/`PUT /invites/:token?org=`, `ApplicationMailer.configured?`, MCP `invite_member` returns `invite_link` (outcome `invited` for someone new, `already_invited` with the link). 03: Invited badge, admin copy-link button (`lib/clipboard.ts` fallbacks), "Revoke invite", `e2e/flows/invite-link.spec.ts`. Reviewed (standards and spec): nothing high; mediums fixed (`eea1ca0`): `invite_link_email`, one `User#invite_url(organization, console_url:)`, Invited cleared by a `User` callback on any password change, a real `:already_invited` outcome. Every invite link, emailed ones too, now uses the host the admin invited from (`request.base_url`), not `CONSOLE_URL`. Left as is: a Dialog, not a popover, for the last copy fallback; `components/copyText.ts` has no fallback. Targeted Rails tests (75 runs), vitest, typecheck and the invite-link and password-reset flows pass; full suites are for the user. It adds a migration (`AddInvitedToUsers`).
- **Release v0.7.0** (2026-10-21, pre-release): the Log transform (`feature/log-transform`); sign-up closed once the install has a user, opened with `instance_settings:set SIGN_UP_OPEN=true`, and log-in and sign-up limited to 10 tries in 3 minutes per IP (`feature/closed-sign-up`, after a security review of the production VM); chart lines that join across empty bins (`fix/chart-lines-join`); and root test helpers that read every page of a Query (`fix/query-helpers-read-all-pages`). Merged on `release/v0.7.0`. Not in it: Dashboard filters (planning only). Two migrations, `CreateLogTransformVersions` and `AddSignUpOpenToInstanceSettings`, which `db_prepare` runs on start; no config changes. EventStore's image now has a Rust stage; its arm64 build is first tried by this release's workflow. Targeted tests on the merge: 117 Rails runs, 440 vitest, typecheck, the vrl/handler/storage Go packages and the Log transform root tests, go vet, and 34 flows (charts, sign-up, Log transform, query editor, project keys, invite link); the full suites were not run.
- **Release v0.6.0** (2026-10-20, pre-release): invite links, merged from `feature/invite-links` (`9435de6`). One migration, `AddInvitedToUsers`, which the Console container's `db:prepare` runs on start; no config changes. Invite emails now carry a link built from the host the admin invited from, not `CONSOLE_URL`. Targeted tests only (75 Rails runs, vitest, typecheck, the invite-link and password-reset flows); the full suites were not run for this release.
- **Release v0.5.0** (2026-10-20, pre-release): switching Organizations from the sidebar and palette (`5ead6e3`), development users seeded only in development and test (`a4da4c6`), `deploy/setup-gcp` asking about the bucket and service account one command at a time (`c058035`), and inviting from the Members page working again (invite links 01). Tagged on `main` after merging `feature/invite-links`. There are no migrations or config changes. Targeted tests only for the invite fix (the Members controller test, vitest, typecheck, a browser check); full suites not run for this release.
- **Release v0.4.0** (2026-10-19, pre-release): request rates on Charts (`rate()`, `peak_rate()`, and each time Chart's bin size), merged from `feature/request-rates` (`c86ee5d`). There are no migrations or config changes. Full run on `main` (2026-10-19):
  - The Go suite passes.
  - Rails passes, 614 runs, on an unseeded test database (`db:test:prepare`). After `db:reset`, the seeds fail 9 self-logs tests.
  - Playwright: 88 of 89 flows passed. `get-started.spec.ts` expected the old `/api/<id>/logs` route, which the Logs snippets left in `95a7342`; it now expects `/api/logs` and passes.
  - The Charts zoom flow was flaky: global setup topped Logs up only below 5,000 in 24 h. It now also tops up when an hour of the last 24, bar the newest two, has none.
- **Release v0.3.0** (2026-10-19, pre-release): Log files go under `logs/` in the bucket, beside `events/` (`49ce346`). Objects uploaded under the old keys aren't moved, so a bucket install must empty its Log files and `logs.duckdb` before upgrading.
- **Release v0.2.0** (2026-10-19, pre-release): images for amd64 and arm64 on ghcr.io, built on native runners by `.github/workflows/release.yml` (also runnable by hand with a tag). `deploy/install` now always pulls a release's images (the checkout's tag, or the newest before it) and stops if a pull fails; `--build` builds from the checkout. The user is installing on GCP T2A (arm64) VMs.
- **SolidTrace's own Logs**: on the VM, `deploy/install` and check the "SolidTrace" Project gets Logs from console, jobs, event_store, caddy and db_prepare ([03](self-logs/issues/03-vector-ships-solidtraces-logs.md)). Shippers inside the VPC send to `http://<internal-ip>:4000`, not Caddy (its certificate is for the public IP).
- **HTTPS**: on the GCP VM, `deploy/setup-gcp` with HTTPS, then a valid lock at `https://<ip>` and an Event sent to `https://<ip>:4443` ([01](https/issues/01-caddy-terminates-tls.md)).
- **MCP server** ([spec](mcp/spec.md)): the whole-effort review found nothing high or medium in the code ([11](mcp/issues/11-whole-effort-review.md), `ready-for-human`). Claude Code was connected to the dev stack's endpoint on `main` (2026-10-18) and called every tool but the key, invite, `create_project` and `run_monitor` ones: all worked. Its findings are in [13](mcp/issues/13-claude-code-check-findings.md) (`needs-triage`); the main one is that `create_monitor` can't make a Monitor that is off. With the user: [bugs 05](bugs/issues/05-production-mail-unconfigured.md), what's left of the production mail settings, and the four Console bugs questions above.

0. **Logs v2** ([spec](logs-v2/spec.md), [tickets](logs-v2/issues/)): built and reviewed; merged into `main` ([23](logs-v2/issues/23-merge-logs-v2.md), resolved). Ticket 20's two measurements are done (2026-10-18, [research](logs-v2/research/dashboard-and-stream.md)): 10,000 stream lines stay smooth on a production build, the 12-chart 7-day Dashboard takes 5.6 s cold and 3.6–4.2 s warm, and compaction stays "not now". Next: the rest of [20, the looks pass](logs-v2/issues/20-looks-pass.md), with the user, and, with the user on `main`, [21, a real GCS run](logs-v2/issues/21-real-gcs-check.md) (needs their bucket and HMAC key). [22](logs-v2/issues/22-leftover-low-findings.md) lists the leftover lows.

1. ~~Click through the Logs explorer in a browser.~~ Done in Logs v2 unit A0 with Playwright; rough edges in [the click-through notes](logs-v2/research/v1-click-through.md).
2. ~~Merge `feature/logs-v1` into `feature/logging` (then `main`) when happy.~~ Merged into `main` with everything else (2026-10-18).
3. [M1-10](logs-v1/issues/M1-10-load-test-on-target-vm.md) (load test) on the 4 vCPU / 16 GB VM: `ready-for-human`, every blocker resolved. Things to measure are in the ticket and in M1-04's and M2-01's Comments.
4. Flagged, not fixed: Log ingest reads a Project's Retention from Console's SQLite on every request, uncached (belongs with the deferred `ValidateKey` cache).

## Efforts

| Effort | Done / open | Start here |
|---|---|---|
| Logs | 16 / 0 | [map.md](logs/map.md) |
| Logs v1 | 19 / 1 | [spec.md](logs-v1/spec.md) |
| Logs v2 | 21 / 3 | [20](logs-v2/issues/20-looks-pass.md) |
| MCP server | 11 / 2 | [11](mcp/issues/11-whole-effort-review.md), [13](mcp/issues/13-claude-code-check-findings.md) |
| Bugs | 10 / 2 | [05](bugs/issues/05-production-mail-unconfigured.md), [10](bugs/issues/10-issues-users-always-zero.md) |
| Prod-readiness | 6 / 0 | [review.md](prod-readiness/review.md) |
| Integration notification | 5 / 0 | [spec.md](integration-notification/spec.md) |
| Ingest write path | 6 / 0 | [spec.md](ingest-write-path/spec.md) |
| SolidTrace's own Logs | 2 / 3 | [spec.md](self-logs/spec.md) |
| HTTPS | 0 / 1 | [01](https/issues/01-caddy-terminates-tls.md) |
| Upgrade Pebble and DuckDB | 3 / 0 | 03 closed as `wontfix` |
| Phone layout | 0 / 1 | [01](phone-layout/issues/01-phone-layout.md) |
| Request rates on Charts | 2 / 0 | merged into `main`, v0.4.0 |
| Log transform | 7 / 1 | [08](log-transform/issues/08-tagalys-sidekiq-program.md) (human, after a release) |
| Zone recovery | 0 / 1 | [01](zone-recovery/issues/01-disk-snapshots-and-restore.md) |
| Log read speed | 0 / 5 | [01](log-read-speed/issues/01-probe-for-log-read-speed.md) ([spec](log-read-speed/spec.md)) |
| Logging-old | closed (wontfix) | replaced by the logs map; reference only |
| EventStore performance | 0 / 2 | [01](eventstore-performance/issues/01-review-performance-findings.md) (after Log read speed), [02](eventstore-performance/issues/02-go-1-27-without-slower-json.md) Go 1.27 ([research](eventstore-performance/research/performance-ideas.md)) |

**Logs path** (a ticket opens when the ones before it are done):
01–16 (done) → logs v1 tickets M1-01 … M3-04

## Last session (2026-10-21, Log transform)

Planned the [Log transform](log-transform/spec.md) with the user (grilling, spec, eight tickets), then built 01 to 07 with `/implement-spec` on `feature/log-transform`, in worktrees merged by merger agents; the dev stack was moved to the integration worktree for the flows. One review (standards and spec), one fixer. Notes and briefs are in `~/.cache/solid-trace/log-transform-notes/`. Also measured production for the user: an hour of `tagalys` Parquet is 563 MB, of which `message` is 367 MB stored and 2,986 MB decompressed (zstd 8.1×); the disk is Hyperdisk Balanced at 3,000 IOPS and 140 MB/s, and n4a-standard-2 caps Hyperdisk at 240 MiB/s. Recommended 240 MiB/s and `n4a-highmem-2` with `duckdb_memory_limit` near 6 GB.

## Earlier session (2026-10-19, request rates)

Built [request rates](request-rates/spec.md) with `/implement-spec`. The two tickets were built at once in worktrees, then merged into `feature/request-rates` by merger agents, which ran the Playwright flows on the dev stack. One review (standards and spec) followed, with one fixer. Notes and briefs are in `~/.cache/solid-trace/request-rates-notes/`. On the way, the dev database had missed the self-logs migration, and the merger ran `db:migrate`. The dev Project also got 20,000 more Logs over the last 24 h.

## Earlier session (2026-10-10 to 17, logs v2)

Planned Logs v2 with the user (Axiom as the reference) and built it from [the spec](logs-v2/spec.md) with workflow batches: a Console track in the main checkout and an EventStore track in a worktree, each unit implemented, reviewed, verified and fixed, then merged with `--no-ff`. On 2026-10-16 the user asked for less token spend: reviews became one review plus one verify, high and medium findings only, with 1 or 2 screenshots per UI unit and no visual polish (looks come later, with the user). A whole-branch review closed the build. Every unit's decisions are in the spec's Implementation decisions; leftovers in [carry-over.md](logs-v2/carry-over.md). Run notes for the orchestrator live in `~/.cache/solid-trace/logs-v2-notes/runs.md`.

## Earlier session (2026-10-02 to 03, logs v1 implementation)

Ran `/implement-spec` on the logs v1 spec, on integration branch `feature/logs-v1`, with at most two sub-agents at a time (the user's limit for this box). Each ticket was built test-first in its own worktree and merged with `--no-ff`; the worktrees are removed.
- **Resolved:** 19 tickets, including **M1-11 (remove Angie)**, added mid-run at the user's request. Console now serves through Thruster and EventStore ingests on TCP 4000.
- **Code review:** a two-axis review (standards and spec) found 8 spec problems, among them a long read stalling Log processing, `contains` missing arrays, a disk-limit default that could delete every Log file, and a clock step back losing acknowledged batches. All were fixed, along with the standards cleanups, in `0eecf81`.
- **Decided by the user (2026-10-03):** Console listens on port 3000; the compose dependency cycle is fixed; ingest bodies are decoded only after the `ingest_max_waiting` gate; every Project edit is admin-only. They are recorded in the spec's Implementation decisions.
- **Lost:** the overnight restart wiped the shared agent notes in `/tmp`. They were rebuilt in `~/.cache/solid-trace/logs-v1-notes/` (setup, briefs, review), outside the repo.

## Earlier session (2026-10-02, logs v1 tickets)

Wrote the logs v1 tickets with `/to-tickets` and resolved [logs 13](logs/issues/13-write-v1-spec-and-first-milestone-tickets.md). The user asked for all three milestones, not only the first, with tickets named by milestone (`M1-01` … `M3-04`).
- **Changed in the spec:** Project deletion is detected when a Project's `projects` row vanishes, not its keys. Keys are hard-deleted, so the old rule would have wiped a Project's Logs when its last key was deleted. Console has no Project destroy route yet.
- **M1-10** (load test) is `ready-for-human`: it needs the 4 vCPU / 16 GB VM and runs past the dev box's one-minute limit.
- No code changed.

## Earlier session (2026-10-02, logs 10 second pass, 15, 16)

[Logs 10](logs/issues/10-storage-architecture-and-retention.md) resolved again (grilling); [ADR 0005](../docs/adr/0005-logs-per-project-hot-tables-promote-at-ingest.md) supersedes ADR 0004, and ADR 0003 gains a note.
- **Decided:** one hot table per Project with key-named promoted columns (up to 100 (type, key) pairs, at ingest); popularity counted by the Log processor per Project per UTC day of `time`; lazy daily re-rank on the first Log of a day; demoted columns dropped at the 01:00 export; a shared MAP-only `logs_late` for out-of-window Logs; the compiler learns columns only from the promoted-set table.
- **SQL mode's `logs` view** now covers the bar's Project and time range (a convenience, not a boundary). **SQL query** in `CONTEXT.md` is amended; tickets 11 and 12 carry pointer comments.
- Reasoned, not benchmarked. The first-milestone load test covers per-Project tables, the daily switch and checkpoints at full scale.
- The map's last two v1 fog patches became tickets and were resolved the same session: [15](logs/issues/15-how-log-queries-run.md) (no per-query timeouts; routing, cancel and caps left to the implementer) and [16](logs/issues/16-explorer-building-blocks.md) (non-Log results as a table only, URL state with SQL never run on load, stream paging, summary line, local time, libraries left to the implementer). Ticket 13 is now the only open logs ticket.

## Earlier session (2026-10-02, DuckDB 1.5.6 and logs 14)

Worked in the main checkout, now on `feature/logging`; all worktrees were removed (prototype branches remain as branches). `main` was merged into `feature/logging`.
- **[Upgrade-pebble-duckdb 02](upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md) resolved** (`9168867`): EventStore runs DuckDB 1.5.6; the alpha wiring is gone; VARIANT layouts in `logsbench` are parked for 2.0. Dev DuckDB data written by the alpha was moved to the session scratchpad, not deleted.
- **[Logs 14](logs/issues/14-benchmark-storage-design-on-1-5-5.md) resolved:** findings in [research/14](logs/research/14-storage-design-bench.md). MAP still beats JSON; stalls remain, no crash; the hot table plateaus; export is fine. The 5 s day target was missed because the hot table has no promoted columns.
- **Decided with the user:** promote keys at ingest, 100 per Project, re-ranked nightly by popularity; the promoted set lives in `logs.duckdb`; plain Parquet files, no DuckLake (probed with a SQLite catalog).
- **Logs 10 reopened** for the remaining promotion questions and a new ADR superseding 0004.
- These commits are on `feature/logging`, not on `main`.

## Earlier session (2026-10-02, logs 12 query modes)

[Logs 12](logs/issues/12-query-builder-ux.md) is resolved (prototype). The user chose a `Builder | Query | SQL` switch. The Builder is filter chips + search only. Query → Builder works only when the Builder can show the Query, otherwise it offers to discard. SQL starts from the compiled SQL and is one-way. **Builder** added to `CONTEXT.md`.
- Prototype: branch `prototype/logs-query-modes` (`7d19a45`), worktree `.claude/worktrees/logs-query-modes`. Its `storage/sqlite/development` is a symlink to the main checkout's dev databases, which now hold the `db:seed` data (`admin@solidtrace.local` / `password123`).

## Earlier session (2026-10-02, logs 09 benchmark)

[Logs 09](logs/issues/09-benchmark-storage-layouts.md) is resolved. A throwaway harness, `event_store/bench/logslayouts`, and the findings ([research/09](logs/research/09-storage-bench-reduced.md)) are on branch `feature/logging` in the logs worktree, not on `main`.
- **Results:**
  - Typed MAPs rank first, then JSON text, then VARIANT without shredding.
  - Shredded VARIANT is unusable on the 2.0 alpha: slow checkpoints and queries, one heap-corruption crash, and a database-invalidating filter error on default-shredded Parquet.
  - Local Parquet gains nothing from async read-ahead.
- **Run:** cut to about 2 minutes per layout at the user's request, about 10 minutes for the whole benchmark.
- **Next for logs:** [10](logs/issues/10-storage-architecture-and-retention.md) (storage and retention grilling) or [12](logs/issues/12-query-builder-ux.md).
- `main` was merged into `feature/logging` on 2026-10-02. The logs tracker conflicts were resolved in favour of `main`, whose map is the live one. The branch's 2026-09-27 MVP scoping stays in its history only.

## Earlier session (2026-10-02, prod-readiness 015)

[Prod-readiness 015](prod-readiness/issues/015-issue-list-counts-and-pagination.md) is resolved (`8b2af22`). Event processing keeps `times_seen`, `first_seen_at` and `last_seen_at` on `issues`, with a replay guard in `project_seen_cursors`. The Issues list is paginated 25 per page, sorts by last or first seen in SQL, and makes no per-Issue EventStore call. The ticket's comment lists what was added beyond the brief.
- Toolchain: run Ruby and Go through `mise exec --`. Run the Go and Rails suites one after the other with `RAILS_ENV=test bin/rails db:reset` in between, because the Go tests leave messages in the shared test message queue.

## Earlier session (2026-10-01, prod-readiness close)

Decided to treat the project as a 0.1 prototype and closed the rest of prod-readiness, keeping only what would change the direction of development.
- **§8:** accepted EventStore writing Console's issue tables. Recorded in [ADR 0001](../docs/adr/0001-eventstore-writes-console-tables.md), the first ADR in `docs/adr/`. The generated enum constants and the startup schema-version check are not done.
- **§7:** filed as [015](prod-readiness/issues/015-issue-list-counts-and-pagination.md), `ready-for-agent` (2026-10-02). EventStore writes the counts directly, with a replay guard in SQLite; no backfill, the test data is wiped. FTS5 and the dead filter controls are out of scope.
- **Closed for now:** 011 and 012 are `wontfix` with a "reopen after the prototype" comment. Review §1, §9 and §10 are marked deferred, and §9 points at logs 10. The `log` envelope fixtures note stays in 012.
- No code changed.

## Earlier session (2026-10-01, ingest write path 04)

A code review of ingest write path 03 filed 04–06. Implemented [04](ingest-write-path/issues/04-duckdb-row-isolation.md): a failed DuckDB append is now classified by asking DuckDB (`Ping()`), not by comparing rows, so a lone bad row is skipped instead of stalling every Project. Content retries back off. duckdb-go returns no typed error for a rejected value, so an append-only failure such as the memory limit still counts as a content error. The ticket's Comments list the details.

## Earlier session (2026-09-30)

Implemented [ingest write path 03](ingest-write-path/issues/03-event-processing.md): Event processing in the new `event_store/processing` package, replacing the DuckDB ingester. One change to the spec: startup no longer moves the processing cursor to DuckDB's newest row. It records that row as an in-memory mark, so a crash after the DuckDB commit still resends the batch's Console messages (story 27). The ticket's Comments list the details.

## Earlier session (2026-09-27)

Mostly housekeeping. Tickets stay as local Markdown, and this file is the status page. Closed logging-old: its spec and all 17 tickets are now `wontfix`, and each one points to the logs map. No logs ticket was claimed or resolved. Then fixed review §6 (Sentry protocol correctness) in `event_store/ingest`. Filed 012 for Sentry compatibility tests. Found that SDKs retry no HTTP status and pause 60 s on a 429, so a full ingest channel now returns 503. Fixed §5, the concurrent first-event race.

Upgraded EventStore and merged it to `main`, recorded in [ticket 01](upgrade-pebble-duckdb/issues/01-upgrade-pebble-and-duckdb.md):
- Pebble v2.1.7, with the format pinned to `FormatValueSeparation` and value separation off.
- DuckDB 2.0 alpha: the `v2.20000.0-6.preview` driver built with `-tags=duckdb_use_lib`, linked to a pinned `libduckdb` from `event_store/scripts/fetch-duckdb` through the `mise.toml` env. Also `OrderedMap` and tests for the time filters and the engine version.
- The Dockerfile now builds with Go 1.25 and cgo.

Follow-ups:
- [02](upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md): move to DuckDB 2.0.0 (due 2026-10-21).
- [03](upgrade-pebble-duckdb/issues/03-pebble-value-separation.md): benchmark Pebble value separation.

The logs map now records that duckdb-go has no real 2.0 build yet.

Designed the ingest write path, picking up the unfinished 2026-09-26 grilling session.
- **Spec:** [spec.md](ingest-write-path/spec.md), `ready-for-agent`. It fixes review §4.
- **Tickets:** 02 and 03 in that effort, plus prod-readiness 013 and 014 (`needs-triage`). 013 was triaged on 2026-09-30, see below.
- **Decisions:**
  - Event ingest only stores the raw Event in Pebble.
  - Event processing owns the Issue lifecycle.
  - Processing cursors live in Pebble. Startup notes how far DuckDB got past each one (changed in 03 from moving the cursor).
  - Console messages are sent at least once.
- **Accepted gap:** a crash can lose an `issue_created`. The spec lists the fixes considered.

## Triage (2026-09-30)

[Prod-readiness 013](prod-readiness/issues/013-archive-overwrites-parquet.md) is now `ready-for-agent`, with an agent brief.
- The overwrite is reproduced. The first draft's trigger was wrong: today, Late Events are stranded in `events_hot`, not lost.
- **Decisions:**
  - `archive_events` carries a cutoff date. Each run moves every Event dated on or before it.
  - Each run writes new `data-<run id>.parquet` files and never overwrites a file.
  - Files are staged as `.tmp`, then the delete commits, then the files are renamed. A check at startup and at the start of each run resolves leftover `.tmp` files.
  - It ships before ingest ticket 03. No glossary change.

[Prod-readiness 014](prod-readiness/issues/014-stuck-processing-messages.md) is now `ready-for-agent`, with an agent brief.
- Confirmed from the code. Also found that the every-second job has no concurrency limit, so overlapping runs can send a message twice.
- **Decisions:**
  - Drop the `processing` claim. A crash leaves the message `pending`, so it is retried.
  - Add `limits_concurrency to: 1` with `on_conflict: :discard` to `EventStoreMessageProcessorJob`.
  - Making the status change and the handling one transaction is ruled out: the message and Solid Queue use separate SQLite files.

## Implementation (2026-09-30)

[Prod-readiness 013](prod-readiness/issues/013-archive-overwrites-parquet.md) is resolved. `ArchiveEventsUpTo(cutoff)` writes new `data-<run id>.parquet` files through `.tmp` staging, and recovery runs at startup and at the start of each run. The ticket's comments list what was added beyond the brief and the one edge case that was accepted.

[Prod-readiness 014](prod-readiness/issues/014-stuck-processing-messages.md) is resolved (`a7c06b2`). The processor job no longer sets `processing`, so a crash leaves the message processable. `processable` also picks up rows already stuck in `processing`. `limits_concurrency to: 1` with `on_conflict: :discard` stops overlapping runs. The concurrency lock expires after Solid Queue's default of 3 minutes, which is accepted under at-least-once delivery.

[Ingest write path 02](ingest-write-path/issues/02-mutex-event-writer.md) is resolved. `PebbleWriter.WriteEvent` stores each Event under one lock before the `200`. `ingest_max_waiting` caps waiting requests with a `503`. The Pebble channel, the Pebble ingester and their settings are gone. The ticket's comments list the accepted shutdown edge cases.
