# 01: User settings' profile form never saves

**Status:** resolved
**Area:** Console
**Found:** 2026-10-17, by the MCP build's ticket 01

## What happens

On User settings, the profile form sends `PUT /user/settings` (`console/app/frontend/pages/Settings/User.tsx:23`). No route matches that path:

```
PUT /user/settings         -> ActionController::RoutingError
PUT /user_settings         -> user_settings#update
PUT /acme/settings/user    -> user_settings#update (org_slug: acme)
```

(This was checked with `Rails.application.routes.recognize_path`, not in a browser.) So saving the name fails, and Inertia shows a routing error instead of the "User settings updated" toast. The path dates from `cd68f20` (2026-02-07), so it predates the logs and MCP work.

## Fix

- Send the form to the route the page was served from: `/:org_slug/settings/user` when there's an org slug, `/user_settings` otherwise. Or use a URL the controller passes as a prop.
- Add a controller test for both paths.
- Add a Playwright step that changes the name and sees the toast.
- The looks pass is restyling this page, so keep the change to the one line.

## Comments

### 2026-10-18 resolved

The form had three faults, not one:

- It sent the wrong URL.
- It sent its fields unnested, while the controller requires `params[:user]`, so even the right URL answered 400.
- `update` built its redirect with `user_path(org_slug: nil)`, which raises on `/user_settings`.

The fixes:

- The page now PUTs to the URL it was served from (`usePage().url`) and nests its fields under `user`, as the Organization settings form does.
- The controller redirects to `/user_settings` when there's no Organization.

`test/controllers/user_settings_controller_test.rb` covers both paths, that the email can't change, and a blank name. Three of its four tests fail without the controller fix. The Playwright flow `e2e/flows/user-settings.spec.ts` saves and restores the name on both paths. It is written but not run: MCP ticket 10 runs it with the other flows.

