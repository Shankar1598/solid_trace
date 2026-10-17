# MCP server: review findings carried over

Findings that a unit's review confirmed only as low, or that a unit's implementer raised, left for later, and where each one goes. Each unit's full finding, with its failure scenario, is in the orchestrator's workflow results (`wf_7acd8b1c-eef`); the summary here is enough to act on.

Status: ready-for-agent

## Scheduled into a later ticket

| From | Finding | Severity | Goes to |
|---|---|---|---|
| 03 | `list_fields` never lists JSON Attributes (arrays such as `tags`) or late Logs' Attributes, because `log_key_counts` counts neither, and its description doesn't say so (03-review-1) | low | 10: say it in the description and `docs/mcp.md` |
| 04 | ADR 0003's Consequences still says the SQL "runs unvalidated", against its own amendment (04-verify-1) | low | 10 |
| 06 | The server's instructions name only `whoami` and `list_projects`, not the logs, Dashboard, Monitor or Issue tools | low | 10 |
| 07 | `create_monitor`'s `kind` description promises that `summarize … by service` fires per group, which needs `notify_by_group` (07-review-1) | low | 10 |
| 01 | The Revoke button stays enabled while its DELETE runs, so a double-click answers a 404 page in Inertia's modal (01-verify-1) | low | 11 |
| 02 | `Base#json` uses `JSON.generate`, so a raw Time or record a tool passes comes out as Ruby's `to_s` or `inspect`, not ISO 8601 (02-review-2) | low | 11 |
| 02 | A `from` or `to` with no offset is read in the server's time zone, not UTC (02-review-3) | low | 11 |
| 05 | A Log line writes the Service raw: a space moves the id out of fourth place, a newline forges a second line, and it isn't cut (05-review-1, 05-verify-1) | low | 11 |
| 05 | `Base#timed_out` reads `body["error"]`, but EventStore's run timeout answers `errors[].message`, so its reason is dropped (05-verify-2) | low | 11 |
| 05 | A short `query_logs` page, cut by EventStore's 1.5 s scan budget, has a header that reads like the whole range (05-review-3) | low | 11 |
| 08 | `get_issue` answers up to 20 comments uncut (08-review-1) | low | 11 |
| 08 | No test ties `get_issue`'s `trace_logs` arguments to `query_logs`'s input schema (08-review-2) | low | 11 |
| 08 | `update_issue` matches `unassigned` case-sensitively, so `Unassigned` is looked up as an email (08-review-3, 08-verify-1) | low | 11 |

## Before the MCP, found on the way

| From | Finding | Goes to |
|---|---|---|
| 01 | User settings' profile form PUTs to `/user/settings`, which no route matches (`/user_settings` and `/:org_slug/settings/user` do). Not checked in the browser. | later |
| 09 | `MemberInvite`, like the controller before it, finds users by email case-sensitively, so inviting `BOB@example.com` when `bob@example.com` is a member makes a second user (09-verify-2) | later |
| 09 | The New Project page still saves a Project with an empty slug or the slug `new`; only `create_project` refuses them | later |

Done in the batch: 09-verify-1 (an empty slug from `create_project`) was fixed with 09-review-1 in `c807d38`, and 02-verify-1 with 02-review-1 in `1186291`.
