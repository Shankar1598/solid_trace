# 01: The Console on a phone

**What to do:** Make every signed-in page usable at phone width (390 px), in the design language of [docs/design-language.md](../../../docs/design-language.md). Split from the looks pass ([logs-v2 20](../../logs-v2/issues/20-looks-pass.md)) on 2026-10-18: the user asked to finish desktop first and treat phones as a separate effort.

**Status:** ready-for-human

## What a phone tour found (2026-10-18)

Run `SCREENS_SET=<name> bunx playwright test --project=screens-phone-dark e2e/screens/tour.spec.ts` on `bin/rails db:seed dev:data`.

- **No navigation.** The sidebar is `hidden md:flex` and nothing replaces it, so below md a signed-in page can't reach any other. Settings' second sidebar is hidden the same way.
- **The Issues list breaks.** With `table-fixed` widths, the Issue column gets no room: titles vanish, Last seen overlaps the badges, and Events and Users run off the right edge. The filter bar overflows too.
- **The Dashboard squeezes its grid.** It keeps twelve columns at 390 px, so the charts read "E…" and "899…" (also carry-over A3).
- **The Issue's event bar** squeezes the event ID and date into four-line columns.
- **The Logs stream** shows only time and service, with the message off to the right (v1 click-through rough edge 22). The inspector is cramped (rough edge 25).
- **Fine as they are:**
  - Log in and Register
  - Members and the Monitor form
  - the Logs bar, whose hint hides below sm
  - tables that scroll in their own box (Monitors)

## A start

[phone-layout.patch](../phone-layout.patch) (`git apply` it) is untested work towards the first three:
- `PhoneNav`: a bar with a menu button that opens the sidebar as a drawer, closing on the next visit.
- `SettingsTabs`: the settings pages as a row of tabs.
- The Issues list's columns at phone width: Issue, Last seen and Events, with search on its own line.
- Dashboard charts stacked in reading order below 640 px.
- The Logs and Settings heights less the 3rem bar.

It was written against the looks pass's step 3; check it with the phone tour before committing.
