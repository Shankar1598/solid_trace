# 02: The sidebar's Organization selector and the palette's Organizations group

**Status:** ready-for-agent
**Spec:** [spec.md](../spec.md)

**What to build:** the frontend half of the spec (Implementation Decisions 5 to 7). It reads the shared prop `organizations: { name: string; slug: string }[]` (sorted by name) that ticket 01 adds beside `current_org`; it can be built against that shape in parallel with 01.

- [ ] `app/frontend/lib/nav.ts` gains a function from a page's URL and another Organization's slug to the URL a switch opens: that Organization's index for the page's `navSection` (Issues, Logs, Dashboards, Monitors, Projects, or Settings, meaning Organization settings), or Issues for a page with no section. Vitest in `nav.test.ts`, covering each section, a Project's Issues and Logs pages, Settings subpages and a page with no section.
- [ ] With two or more Organizations, the sidebar header becomes a dropdown (the existing dropdown menu component) listing them by name, a check mark on the current one, each linking to where the switch lands. With one, the header renders as it does today.
- [ ] A palette source (`lib/palette.ts` registry) adds an "Organizations" group: the user's other Organizations, each going to its Issues.
- [ ] Typecheck and the relevant vitest pass.
