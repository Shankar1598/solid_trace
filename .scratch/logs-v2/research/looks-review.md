# Looks review: every Console page (2026-10-17)

Design review for [ticket 20](../issues/20-looks-pass.md) against the chosen direction **B**:
- IBM Plex Sans for the interface, JetBrains Mono for data.
- Square corners; circles stay round.
- A neutral palette, with colour only for meaning.
- One page header, and panels that are outlined rather than filled.
- Sentence case everywhere.
- Rounded tabular numbers.

The direction page is at https://claude.ai/artifact/K6onrwpFyjBpuhWtW599HH.

Source: `console/e2e/screens/tour.spec.ts` on `bin/rails db:seed dev:data` (commit `3219353`). Screenshots are in `~/.cache/solid-trace/screenshots/tour-data/`: 23 pages at desktop size, dark and light.

## App-wide (fix once, in shared code)

1. **Type.** Every word is JetBrains Mono (`--font-sans` in `css/globals.css`). Switch the interface to IBM Plex Sans. Data stays mono: timestamps, IDs, values, log messages, queries, stack frames, DSNs, slugs and numbers in tables.
2. **Page header.** There are six patterns today:
   - Issues: the title sits at the top with no actions.
   - Dashboards, Monitors and Projects: the title sits about 75px down, with the action on the right.
   - Settings pages: a large title with a large subtitle.
   - Issue, Monitor, Project new and Monitor edit: breadcrumbs above the title.
   - Dashboard: a smaller title, a description, and a toolbar row of 5 buttons plus a second row holding only "Edit layout".
   - Logs: no header at all.

   One `PageHeader` should cover all of them: optional breadcrumbs, title, optional one-line description, actions on the right, and the same top offset everywhere.
3. **Surfaces.** Grey filled cards are used on:
   - Settings and Project show
   - the Projects index cards
   - the Monitors empty state
   - Issue statistics and comments
   - Integration rows

   The Issues table, the Logs panel and the chart cards are outlined. Make panels outlined on the page background, and keep fill for what floats (menus, popovers, the inspector) and for code blocks.
4. **Tables.** Header rows come in four styles:
   - UPPERCASE and letterspaced (Issues)
   - tiny bold UPPERCASE (Issue → Events)
   - sentence case on a filled band (Dashboards, Monitors, Monitor show)
   - lowercase (Logs, dashboard table chart)

   Use one table style: sentence-case headers, no fill and a hairline, with numbers right-aligned and tabular.
5. **Badges and status.** There are seven looks today:
   - UNRESOLVED as a red uppercase chip
   - Firing as a red chip
   - OK as a grey chip
   - `error` and `default` as outlined chips beside the red text UNHANDLED
   - STAGING and PRODUCTION as uppercase chips
   - ADMIN as blue text inside a select
   - Logs levels as coloured uppercase text

   Replace them with one `Badge` with tones neutral, info, warn, error and ok, in lowercase or sentence case. Status gets a round dot.
6. **Section labels.** These all use different sizes, weights and padding:
   - ISSUE STATISTICS, EXCEPTION, USER CONTEXT, TAGS, HISTORICAL EVENTS (Issue)
   - ORGANIZATION and USER (Settings nav)
   - SETTINGS (Settings sidebar header)

   Use one small-caps label style, or a sentence-case `h3`.
7. **Button text.** Title Case appears in Save Changes, Create Project, New Project, Add Integration, Post Comment, Send Invitation and Delete Project. Sentence case appears in New monitor, New dashboard, Add chart and Run now. Use sentence case throughout.
8. **Brand and accent.** The app shows a red "S" tile, while Login and Register show a white circle. The avatar and the dark sidebar's primary colour are violet (`--sidebar-primary`). Use one monochrome mark and drop the violet.
9. **Corners.** These are still rounded:
   - sidebar nav items and Search
   - the Builder/Query switch and the Kind switch
   - stack-trace frames
   - the invite box
   - the Dashboards and Monitors table outlines
   - Copy link and other small buttons

   Zero `--radius` and clear the arbitrary radii, as the ticket says.
10. **Numbers.** The dashboard table shows `544.2391475211607` and `4485.272743930371`. The Errors stat is labelled with the raw column `count_`. Latency axes show `6K` with no unit. Use one display formatter for tables, stats and tooltips.
11. **Form spacing.** A card's description runs straight into its first field label in these places:
    - Settings → General and Account
    - Project new
    - Project Log retention
    - Integration edit, where "Slack Configuration" appears twice, once as the card title and once as a section heading

    The Kind segmented control is taller than the inputs beside it. Use one form-section layout: title, description, a gap, then fields on a fixed rhythm.

## Page by page

- **Logs:**
  - It has no page title. Keep the full-bleed panel, but give it the shared header or a deliberate equivalent.
  - The level column uses coloured UPPERCASE text, while the histogram legend uses lowercase dots. Make them agree.
  - The selected row's expand chevron overlaps the timestamp.
- **Issues index:**
  - Each row stacks four styles under the title: a level chip, red UNHANDLED, the project, and a culprit in a different grey.
  - Age reads "1 hour" on every row, and Users shows 0 although every event carries a user (see Bugs).
- **Issue show:**
  - The Overview, Events and Comments tabs stretch across the full width, while Logs' Table and JSON tabs sit left. Make tabs left-aligned everywhere.
  - The event ID wraps over two lines.
  - The statistics panel duplicates the header's status.
  - The comment box draws a double border.
- **Dashboards index:**
  - Four "Flow dashboard 1792…" rows are left over from e2e flows (see Bugs).
- **Dashboard:**
  - The toolbar wraps "Edit layout" and the settings gear onto a second row.
  - Each chart header repeats "SolidTrace Project".
  - The stat chart labels itself `count_`.
- **Monitors index and show:** these are the closest to target. Only the header, table and badge fixes apply.
- **Monitor new and edit:**
  - The page is very wide and dense, with a long help sentence under Name.
  - "A run now" reads as a heading for a non-sentence.
- **Projects index:** two cards fill a third of the width. Use a table like the other indexes (name, platform, Issues, created).
- **Project show:**
  - Platform and Project ID are shown as disabled inputs; show them as read-only values.
  - The Danger zone uses a different text size.
- **Settings:**
  - Its second sidebar has its own grey "SETTINGS" header band.
  - Each page's large title and subtitle compete with the card title and description, so every page carries two headings and two descriptions.
- **Integrations:**
  - Rows are tall filled cards with initials avatars (SL, PA, EM). "Pagerduty" should be "PagerDuty".
  - Configure and Delete are styled differently from row actions on other pages.
- **Login and Register:** in dark mode, Register's brand panel is light grey with white text and is unreadable. Login in light mode shows a dark panel. The panel should keep its own colours in both themes. The quote is wrapped in literal quotation marks.

## Bugs seen on the way (not looks; file separately)

- **Issue first seen is in the future.** It reads "First seen: in about 1 hour", and Age is "1 hour" on every Issue, while last seen is 3–8 h ago.
- **Users column is 0** on the Issues index for Issues whose Events carry a user. Cause: `pages/Issues/Index.tsx` renders a hard-coded `format(0)`.
- **Age counts from the Console row, not the first Event.** The Issues index's Age is `formatDistanceToNow(issue.created_at)`, so Issues ingested in a batch all read the same age.
- **The Issues index's time filter does nothing.** In `pages/Issues/Index.tsx` the range select (24 hours, 14 days, 30 days) only has a `defaultValue="14d"`; unlike the project and sort selects it never calls `visitFilters`, so the list is the same whatever range is picked. Connect it, or hide it until it works. The user asked (2026-10-17) for this to be fixed after the UI work, with the other bugs here.
- **The Issues index's checkboxes select nothing.** `pages/Issues/Index.tsx` draws a select-all box and one per row, with no state and no bulk action (resolve, assign). Wire them to bulk actions or remove them.
- **Delete project does nothing.** The Danger zone's button in `pages/Projects/Show.tsx` has no handler. Confirm and delete, or remove the button.
- **Forgot your password? opens a blank page.** `PasswordsController#new` and `#edit` render the Inertia components `passwords/new` and `passwords/edit`, which don't exist, so the client throws "Page not found". Add the two pages (email form, new-password form) in AuthLayout, or hide the link.
- ~~**Every unresolved Issue is labelled UNHANDLED.**~~ Fixed in step 2: the row shows a Resolved badge on resolved Issues and nothing else, since an Issue doesn't record whether its exception was handled.
- **e2e flows leave dashboards behind:** "Flow dashboard <timestamp>" rows stay after the flows run.

## Order of work

1. **Foundation:**
   - fonts (B)
   - radius tokens
   - neutral tokens: drop the violet, use one brand mark
   - the number display formatter
2. **Shared components:**
   - `PageHeader`
   - one table style
   - `Badge`
   - section label
   - outlined panel
   - form-section layout
   - a sentence-case sweep of buttons and headings
3. **Pages**, with before-and-after screenshots for the user, in this order:
   1. Logs
   2. Dashboards and Monitors
   3. Issues
   4. Projects
   5. Settings
   6. Login and Register
