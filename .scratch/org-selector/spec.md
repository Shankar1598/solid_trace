# Org selector

**Status:** ready-for-agent

Planned with the user on 2026-10-19, after they could not reach the Meta Organization's Logs on their VM install: Console has no way to move between Organizations.

## Problem Statement

A user who belongs to more than one Organization can only ever see the first one. The sidebar header names the current Organization but does nothing, the command palette lists only the current Organization's Projects, Dashboards and Monitors, and `/` always opens the user's first membership. Every VM install now has a Meta Organization beside the one its people work in, so this hits every install: the only way to SolidTrace's own Logs is to type its URL.

## Solution

With two or more Organizations, the sidebar header opens a dropdown of the user's Organizations. Picking one opens the same section there: from a Project's Logs in one Organization to the other Organization's Logs. The command palette can jump to any of them too. Console remembers the Organization the user last opened, so `/` takes them back to it rather than to the first one.

## User Stories

1. As a user in two Organizations, I want the sidebar header to list my Organizations, so that I can see which ones I can open.
2. As a user in two Organizations, I want to pick another Organization from the header, so that I can work in it without typing its URL.
3. As a user, I want the current Organization marked in the list, so that I know where I am.
4. As a user, I want my Organizations listed by name, so that I find one quickly however many I have.
5. As an admin of a VM install, I want the Meta Organization listed like any other Organization, so that I reach SolidTrace's own Logs the same way I reach my own.
6. As a user reading Logs in one Organization, I want switching to open the other Organization's Logs, so that I keep doing what I was doing.
7. As a user on a Project's page, I want switching to open the other Organization's Projects, since that Project isn't there.
8. As a user on a Dashboard or Monitor, I want switching to open the other Organization's Dashboards or Monitors, since that one isn't there.
9. As a user in Settings, I want switching to open the other Organization's Settings.
10. As a user on a page with no section, I want switching to open the other Organization's Issues.
11. As a user in one Organization, I want the header to stay as it is now, with no dropdown, so that nothing offers a choice I don't have.
12. As a keyboard user, I want an "Organizations" group in the command palette, so that I can jump to another Organization without the mouse.
13. As a user, I want the palette to leave out the Organization I'm in, so that each entry takes me somewhere new.
14. As a user who last worked in the Meta Organization, I want `/` to open it again, so that signing in or following the logo brings me back where I was.
15. As a user who opened an Organization from a bookmark or a shared link, I want that to count as using it, so that `/` follows where I actually was, not only what I picked in the selector.
16. As a user removed from the Organization I last used, I want `/` to open my first Organization by name, so that I never land on a 404.
17. As a user whose last used Organization was deleted, I want `/` to fall back the same way.
18. As a user left in no Organization, I want `/` to keep opening Make an Organization, as it does today.
19. As a user, I want an Organization I'm not a member of to stay a 404 whether I reach it from the selector, a link or a typed URL, so that the selector exposes nothing new.
20. As a user signing in on a new browser, I want `/` to open my first Organization by name, so that a lost cookie still sends me somewhere sensible.

## Implementation Decisions

- **One lookup for the current Organization.** Thirteen controllers each find the current Organization among the signed-in user's Organizations by the URL's slug. That moves into one concern, which every controller under an Organization's URL uses. It finds the Organization the same way, so another Organization's slug is still a 404, and it records the Organization as last used.
- **Last used Organization in the session cookie.** It is the id of the last Organization whose page the user opened, by any route. No column or migration; losing the cookie only means falling back.
- **`/` opens the last used Organization's Issues.** It is used only while the user is still a member. Otherwise `/` opens the user's first Organization by name, and with none, Make an Organization, as today. Ordering by name replaces today's unordered first membership.
- **Shared props gain the user's Organizations.** Every page under an Organization shares the signed-in user's Organizations as name and slug, sorted by name, beside the current Organization. The palette and the sidebar both read them.
- **Where a switch lands.** The frontend nav module gains a function from a page's URL and another Organization's slug to that Organization's index for the page's section: Issues, Logs, Dashboards, Monitors, Projects, or Settings (Organization settings). A page with no section lands on Issues. It builds on the existing section function, which already maps a Project's Issues and Logs pages to the Issues and Logs sections.
- **The sidebar header is a dropdown with two or more Organizations.** It uses the existing dropdown menu component. Each item shows the Organization's name, with a check mark on the current one, and links to where the switch lands. With one Organization the header renders as it does today.
- **A palette source for Organizations.** A new "Organizations" group lists the user's other Organizations, each going to its Issues.
- **Make an Organization is not in the selector.** Its page and wording stay as they are.
- **Glossary.** `CONTEXT.md` defines **Organization**: a user can belong to several and works in one at a time.

## Testing Decisions

- Tests check what a user or a request sees, such as redirects, statuses, shared props, the URL a switch opens and what the page shows. They don't check how the concern or the components are built.
- **Rails request tests**, alongside the existing controller tests:
  - `/` opens the last used Organization.
  - `/` falls back to the first by name when the user was removed from the last used one, when it was deleted, and when there's no cookie.
  - `/` goes to Make an Organization with none.
  - Opening any Organization page records it.
  - Shared props list the user's Organizations by name, and only theirs.
  - Another Organization's slug is still a 404 on a few representative controllers after the lookup moves.

  Prior art: the home and organizations tests, and the projects and logs controller tests' 404 cases.
- **Vitest on the nav module**: where a switch lands, from each section's pages, including a Project's Issues and Logs pages, Settings subpages, and a page with no section. Prior art: the nav module's existing tests.
- **One Playwright flow.** A user in two Organizations switches from the header on a Logs page and lands on the other Organization's Logs. `/` then keeps them there. The palette's Organizations group jumps back. A user in one Organization sees no dropdown. No screenshots. Prior art: the palette flow and the make-organization flow, which already makes a second user and Organization.
- Run only the relevant tests. The user runs the full suites.

## Out of Scope

- Make an Organization from the selector, and changing that page's wording.
- A mobile selector. Below the `md` width there's no sidebar or mobile nav at all.
- Keeping the same Project, Dashboard or Monitor across a switch.
- Storing the last used Organization on the user record, or across browsers.
- Marking the Meta Organization differently.

## Further Notes

- An earlier fix on 2026-10-19 (`a4da4c6`) stopped `db:seed` making the development users and Organization on production. That seeding was why the user's install had a third Organization, and why its Meta Organization's first admin wasn't them.
- No ADR: the cookie is easy to replace, and none of the other decisions is hard to reverse.
