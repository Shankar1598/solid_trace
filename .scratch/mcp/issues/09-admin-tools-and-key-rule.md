# 09: Admin tools and the key rule

**Status:** resolved
**Track:** Console
**Blocked by:** 02

## What to build

The Admin row of the spec's Tool catalogue, and keys becoming admin-only in the UI too, as decided by the user on 2026-10-17.

**Key rule.**
- `ProjectKeysController` authorizes create, rotate and delete through a policy that allows only the Organization's admins.
- The Project page hides those buttons for members.
- The MCP key tools use the same policy.
- Creating a Project stays open to any member.

**Invite service.** The flow in `OrganizationUsersController#create` moves into a service, and both the controller and `invite_member` call it: find or create the user, reactivate a discarded membership, create the membership, send the invitation email. It stays admin-only through `UserPolicy` and `OrganizationUserPolicy`.

**Tools:**
- `create_project` gives the Project its first key, as `ProjectsController#create` does.
- `get_project_setup` returns the DSN, the Log ingest URL and key, and a curl example, with the same values the Project and Logs pages show.
- `create_key`, `rotate_key` and `delete_key`. Rotate and delete are `destructiveHint`.
- `list_members` and `invite_member`.

## Acceptance

- Tool tests: a member's `rotate_key` and `invite_member` are refused with "only an Organization admin can …", and an admin's work.
- Controller tests for the new key policy.
- The Playwright flow that covers the Project page's keys, updated for members.
- `bin/rails test` passes.

## Comments

### 2026-10-17 resolved

Built on `feature/mcp` in workflow `wf_7acd8b1c-eef` (implement, one review and one verify, a skeptic on high and medium findings, fixes, recheck). Head `c807d38`. One finding confirmed and fixed: `create_project` made a Project with an empty slug (a name with no Latin letters or digits) or the slug `new`. The build also found that `ProjectKey#rotate!` was never defined, so Rotate always raised; it is now. The Playwright flow `e2e/flows/project-keys.spec.ts` is written, not run: ticket 10 runs it. Decisions are under 09 in the spec's Implementation decisions; findings left for later are in [carry-over.md](../carry-over.md).
