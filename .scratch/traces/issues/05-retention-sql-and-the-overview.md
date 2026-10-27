# 05: Trace Retention settings, Spans in SQL, and the Overview

Spec: [../spec.md](../spec.md)

**What to build:** Spans join the rest of SolidTrace.

- **Settings:** trace Retention sits beside Log Retention on Settings → Instance (Instance admins) and in the Project's settings (admins). `get_instance_settings`, `update_instance_settings`, `list_projects` and `update_project` read and write it.
- **SQL:** SQL queries, in the explorer and `run_sql`, can read a `spans` table of the Project's Spans behind the same read-only guard as `logs` and `events`. The SQL docs and the tool's description list its columns.
- **Overview:** the page and `get_overview` show Spans received and stored, per Project and in total. Span ingest is counted without the Overview route reading `spans`. Compared with other tools gains a Datadog APM list-price estimate, researched with sources and dated.

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] The settings save, validate and are refused to non-admins, over HTTP and MCP (Rails controller + MCP tool tests)
- [ ] A `spans` query reads its own Project's Spans only (Go, beside the SQL scope tests)
- [ ] The Overview route answers Spans received and stored, and `get_overview` includes them with the APM estimate (Go + MCP tool test)
- [ ] Playwright flows change trace Retention and see Spans on the Overview
