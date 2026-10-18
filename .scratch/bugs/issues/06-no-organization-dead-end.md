# 06: A user with no Organization signs in to a dead end

**Status:** resolved
**Area:** Console
**Found:** 2026-10-18, by MCP ticket 10's run of the password-reset flow

## What happens

`HomeController#index` sends a user to their first Organization's Issues. A user with no Organization gets `render plain: "Welcome! Please create an organization."` instead (`console/app/controllers/home_controller.rb`, unchanged since `6688f7b`). Console has no page or route that makes an Organization.

The sign-in and sign-up forms post through Inertia, so the plain-text answer isn't an Inertia page. Inertia shows it in its error modal over the form, and the URL stays at `/login` or `/registration/new`. Nothing on it leads anywhere.

Users with no Organization:

- **Everyone who signs up.** `RegistrationsController#create` makes the user and signs them in, but makes no Organization.
- **A member archived from their only Organization.** `organization_users` is scoped to kept rows.
- Users made in the console or by `rails runner`, such as the flow's own user before the fix below.

Found when `e2e/flows/password-reset.spec.ts` signed in with its new password and never left `/login`. The flow's user now joins the seed Organization, so the flow tests the reset and not this.

## To decide

What a user with no Organization should see. Some options:

- **Sign-up makes an Organization,** named after the user or asked for on the form, with them as its admin. That covers new users. Archived members still need a page.
- **A "no Organization yet" page** (an Inertia page, so it renders in place). It says to ask an admin for an invite, and has a form to make an Organization, if anyone may.
- **Close sign-up** and make invites the only way in, if open sign-up isn't wanted on a self-hosted install.

## Comments

### 2026-10-18 resolved

The user asked for a fix. This builds the page `HomeController`'s comment had planned ("a 'create org' page"). It covers everyone in no Organization: new sign-ups, archived members, and users made by hand.

- **Home.** `HomeController` sends a user in no Organization to **Make an Organization** (`/organizations/new`), and the rest to their first Organization's Issues, as before. It keeps the flash, so sign-up's "Welcome!" shows on the page it lands on.
- **The page.** It's an Inertia page, `Organizations/New`, in the sign-in pages' layout, so it renders in place of the form, not in Inertia's error modal. It asks for a name, says to ask an admin for an invite to join someone else's Organization, and has Log out.
- **Making one.** `Organization.make_for(user, name:)` saves the Organization and makes the user its admin, in one transaction. A blank name comes back with its error. Success opens the new Organization's Issues: "<name> is made. Add a Project to send it Events and Logs."
- **The slug comes from the name.** A taken slug gets a number (`acme-2`), rather than an error, since slugs are global and a user can't see other Organizations. So does a slug that one of Console's top-level paths uses, since an Organization's pages are under `/:org_slug`: `/passwords/issues` would be a reset link. The paths are read from the routes: `api_tokens`, `cable`, `health`, `login`, `mcp`, `organizations`, `passwords`, `rails`, `registration`, `session`, `up` and `user_settings`. A name with no Latin letters or digits gets `organization`, and underscores become hyphens.

**Found on the way, and fixed: signing up never worked in the browser.** `Registrations/New` posted its fields unwrapped. Rails wrapped them as `registration`, after the controller, and `RegistrationsController#user_params` requires `user`, so every sign-up answered `400 ParameterMissing`. It's the same mistake as [bugs 01](01-profile-name-never-saves.md)'s profile form, and fixed the same way: the form wraps its fields under `user`. The controller tests had posted `user: {…}` themselves, so only the browser showed it.

Tests:

- `test/models/organization_test.rb`, 6 new cases: admin and slug, a taken slug, top-level paths, a non-Latin name, underscores, and a blank name making no membership.
- `test/controllers/organizations_controller_test.rb`, 8 cases:
  - home, for a user in no Organization, an archived member, and a member as before;
  - sign-up leading to the page with the welcome kept;
  - the Inertia page;
  - making one;
  - a blank name;
  - signed out.
- `e2e/flows/make-organization.spec.ts`: signs up in the browser, lands on Make an Organization with the welcome, makes one, and lands on its Issues. It failed at sign-up's 400 before the form's fix.

Rails (589 runs), vitest (391) and typecheck pass. Every Playwright flow passed but one. user-settings timed out on a page that never rendered, while the box was busy with another session's tests; the server had answered it in 39 ms. Run again on its own, it passed 4 times out of 4.
