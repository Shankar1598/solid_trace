# 01: API tokens

**Status:** resolved
**Track:** Console
**Blocked by:** none

## What to build

Personal API tokens, as in the spec's Design, under Tokens. A user makes them in User settings, and the MCP server reads them from `Authorization: Bearer`.

- Migration and `ApiToken` model:
  - `user_id`, `name`, `token_digest` (SHA-256, unique), `last_used_at` and timestamps.
  - The secret is `st_` plus at least 32 random bytes. It is shown once and never stored.
  - Lookup is by digest.
  - `touch_last_used` writes at most once a minute.
- User settings (`console/app/frontend/pages/Settings/User.tsx`, `UserSettingsController` or a small `ApiTokensController`):
  - make a token by name;
  - show it once with copy-paste snippets for Claude Code (`claude mcp add --transport http solidtrace <console>/mcp --header "Authorization: Bearer …"`) and Cursor (`mcp.json`);
  - list tokens by name, creation and last use;
  - revoke a token, which deletes its row.
- Remove the dead `api/v1` routes in `console/config/routes.rb`; their controllers don't exist. Keep `Api::BaseController` only if something uses it.

## Acceptance

- Model tests: the digest lookup works, a revoked token isn't found, and `last_used_at` is throttled.
- Controller tests: a user sees and revokes only their own tokens.
- Playwright flow `console/e2e/flows/api-tokens.spec.ts`: make a token, see it once with the snippet, see it listed, revoke it.
- `bin/rails test`, vitest and typecheck pass.

## Comments

### 2026-10-17 resolved

Built on `feature/mcp` in workflow `wf_7acd8b1c-eef` (implement, one review and one verify, a skeptic on high and medium findings, fixes, recheck). Head `d49eca0`. No finding confirmed above low. The Playwright flow `e2e/flows/api-tokens.spec.ts` is written, not run: ticket 10 runs it. Decisions are under 01 in the spec's Implementation decisions; findings left for later are in [carry-over.md](../carry-over.md).
