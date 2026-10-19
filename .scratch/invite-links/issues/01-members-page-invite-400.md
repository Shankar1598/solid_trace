# 01: Inviting from the Members page fails with a 400

**Status:** ready-for-agent
**Spec:** [spec.md](../spec.md)

**What to build:** the fix for the 400 alone, so that it can land before the rest.

The page (`console/app/frontend/pages/Settings/Members.tsx`, `submitMember`) posts `{ email, name }`. `OrganizationUsersController#create` reads `params.require(:member)`, and Rails wraps the JSON under `organization_user`, so every invite raises `ParameterMissing` (400).

- [ ] The page sends `{ member: { email, name } }` (for example with `useForm`'s `transform`), matching the controller and its tests.
- [ ] A request test that posts JSON the way Inertia does (`as: :json`, with the `member` wrapper) and gets the redirect with "Member added successfully". Also check that a flat body is still a 400, so the test would catch the page drifting again. Prior art: `test/controllers/organization_users_controller_test.rb`.
- [ ] Check it in the browser (Playwright MCP or a quick flow): inviting from the Members page adds the row.

## Comments
