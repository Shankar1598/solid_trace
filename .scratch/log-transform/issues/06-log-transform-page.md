# 06: The Log transform page: edit, save, restore

**What to build:** A Log transform page under the Project, linked from the Project's settings, where an admin edits the program with errors underlined as they type, saves it after a confirmation, sees the running counts, and restores an earlier version. Members see it read-only. See the spec's "The page".

**Blocked by:** 04, 05

**Status:** ready-for-agent

- [ ] The Project's settings page links to the Log transform page.
- [ ] A CodeMirror editor underlines errors and warnings from the check route at their byte spans, debounced as the Query editor validates.
- [ ] Save asks for confirmation, saying stored Logs don't change, and saves through the shared save service; a program that doesn't compile is refused with its errors.
- [ ] The running counts, with the failed count linking to the Logs explorer with `isnotnull(solidtrace.transform_error)`; a compile error from the stats is shown prominently.
- [ ] The last 20 versions, with who saved each and when, a way to view each, and Restore.
- [ ] Members get the editor read-only, with no Save or Restore; a member's save is refused by the controller.
- [ ] Rails controller tests: admins save and restore, a member's save is refused, the page gives members the program read-only, errors are passed back.
- [ ] A Playwright flow: an admin opens the page from settings, types a program with an error and sees it underlined, fixes and saves it, sends a test Log and finds the new Attribute in the Logs explorer, restores the previous version; a member sees the page read-only. No screenshots.
