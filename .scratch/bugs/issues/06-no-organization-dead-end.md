# 06: A user with no Organization signs in to a dead end

**Status:** needs-triage
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
