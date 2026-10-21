# 05: Agents read, preview and change a Log transform over MCP

**What to build:** Console keeps a Project's Log transform versions and saves new ones through one service, and an AI agent reads, previews and changes the Log transform through three MCP tools. See the spec's "Console" (versions table, saving, who may do what) and "MCP tools".

**Blocked by:** 02

**Status:** resolved

- [ ] The version model: the newest version is current; `user_id` becomes null when that user is gone; deleting a Project deletes its versions.
- [ ] One save service (used later by the page too) runs EventStore's check first and refuses with its errors, saving nothing; saving the same text as the current version does nothing; restoring is saving an old version's text. Only admins may save (`ProjectPolicy#update?`).
- [ ] The EventStore client gains check, preview and stats calls matching the route shapes in the spec.
- [ ] `get_log_transform(organization?, project, version?)`, read-only: the current or given version's program, id, who saved it and when, the last 10 versions (id, who, when, size) and the running counts.
- [ ] `preview_log_transform(organization?, project, program, logs?)`, read-only for any member: the check's errors, or each Log's before and after (the first 20 in full) with kept, dropped and failed.
- [ ] `update_log_transform(organization?, project, program)`, destructive and admin-only: saves a version or is refused with VRL's formatted errors; a member is told only admins can change it.
- [ ] The descriptions explain the shape of `.`, `abort`, `solidtrace.transform_error`, the missing functions, that a save applies only to Logs processed after it, and link VRL's function reference. `docs/mcp.md` lists the tools.
- [ ] Each answer ends with the Log transform page's link (the page itself comes in 06).
- [ ] Rails tests with EventStore stubbed by WebMock: the model and service cases, the three tools, admin against member, errors passed through.

## Answer

Built and merged (`27e5313`, 2026-10-21). `LogTransformVersion` and `Project#log_transform`; `LogTransformSave.call(project:, user:, program:)` returning `:saved`, `:unchanged` or errors (raises `Pundit::NotAuthorizedError` for members); `EventStore#check_log_transform`, `#preview_log_transform`, `#log_transform_stats`; MCP `get_log_transform`, `preview_log_transform`, `update_log_transform` sharing `LogTransformTool`; the page route `/:org/projects/:project/log_transform` (controller in 06); `docs/mcp.md`. Decisions: a blank program saves without the check; a preview's errors are accepted from a 200 or a 400; preview answers list up to 10 distinct failure messages with counts beyond the first 20 results. 96 targeted Rails runs pass.
