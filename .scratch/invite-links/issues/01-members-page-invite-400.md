# 01: Inviting from the Members page fails with a 400

**Status:** resolved
**Spec:** [spec.md](../spec.md)

**What to build:** the fix for the 400 alone, so that it can land before the rest.

The page (`console/app/frontend/pages/Settings/Members.tsx`, `submitMember`) posts `{ email, name }`. `OrganizationUsersController#create` reads `params.require(:member)`, and Rails wraps the JSON under `organization_user`, so every invite raises `ParameterMissing` (400).

- [x] The page sends `{ member: { email, name } }` (for example with `useForm`'s `transform`), matching the controller and its tests.
- [x] A request test that posts JSON the way Inertia does (`as: :json`, with the `member` wrapper) and gets the redirect with "Member added successfully". Also check that a flat body is still a 400, so the test would catch the page drifting again. Prior art: `test/controllers/organization_users_controller_test.rb`.
- [x] Check it in the browser (Playwright MCP or a quick flow): inviting from the Members page adds the row.

## Comments

Resolved on `feature/invite-links` (merge `c870a2f`, fix `46f27e5`). The page sends `{ member: { email, name } }` through `useForm`'s `transform`; no controller change was needed. New request test "an admin invites with JSON the way the Members page posts it" posts JSON with the wrapper (redirect, "Member added successfully") and a flat body (400, no user made). Checked on the dev stack: an invite from the Members page posted `{"member":{...}}`, got a 302, and the row and toast appeared. Released in v0.5.0.
