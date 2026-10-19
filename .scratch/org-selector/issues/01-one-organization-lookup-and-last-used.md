# 01: One Organization lookup, the last used Organization and shared Organizations

**Status:** resolved
**Spec:** [spec.md](../spec.md)

**What to build:** the Rails half of the spec (Implementation Decisions 1 to 4).

- [x] One concern finds the current Organization among the signed-in user's by the URL's `org_slug` (`find_by!`, so another Organization's slug is still a 404) and records its id in the session as the last used Organization. The 13 controllers that each do `current_user.organizations.find_by!(slug: params[:org_slug])` (grep for it; `user_settings_controller.rb` does it conditionally) use the concern instead.
- [x] `HomeController#index` (`/`) opens the last used Organization's Issues while the user is still a member of it; otherwise the user's first Organization by name; with none, `new_organization_path`, as today.
- [x] `app/controllers/concerns/inertia_share.rb` shares `organizations`: the signed-in user's Organizations as `name` and `slug`, sorted by name, on every page with a current Organization, beside `current_org`.
- [x] Rails request tests, per the spec's Testing Decisions (home, organizations, projects and logs controller tests are prior art).

Ticket 02 reads the `organizations` prop as `{ name: string; slug: string }[]`.

## Comments

- 2026-10-20: Done on `org-selector-01`. `CurrentOrganization` (`app/controllers/concerns/current_organization.rb`, in `ApplicationController`) gives `set_current_org` (the 13 controllers' before_action; `user_settings` only with an `org_slug`) and `home_organization` for `/`. The last used Organization is `session[:last_organization_id]`. `organizations` is shared as `[{name, slug}]` by name, only when there is a current Organization. Tests: new `test/controllers/home_controller_test.rb`; the touched controllers' existing tests pass.
