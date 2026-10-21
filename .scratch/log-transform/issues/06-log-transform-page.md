# 06: The Log transform page: edit, save, restore

**What to build:** A Log transform page under the Project, linked from the Project's settings, where an admin edits the program with errors underlined as they type, saves it after a confirmation, sees the running counts, and restores an earlier version. Members see it read-only. See the spec's "The page".

**Blocked by:** 04, 05

**Status:** resolved

- [ ] The Project's settings page links to the Log transform page.
- [ ] A CodeMirror editor underlines errors and warnings from the check route at their byte spans, debounced as the Query editor validates.
- [ ] Save asks for confirmation, saying stored Logs don't change, and saves through the shared save service; a program that doesn't compile is refused with its errors.
- [ ] The running counts, with the failed count linking to the Logs explorer with `isnotnull(solidtrace.transform_error)`; a compile error from the stats is shown prominently.
- [ ] The last 20 versions, with who saved each and when, a way to view each, and Restore.
- [ ] Members get the editor read-only, with no Save or Restore; a member's save is refused by the controller.
- [ ] Rails controller tests: admins save and restore, a member's save is refused, the page gives members the program read-only, errors are passed back.
- [ ] A Playwright flow: an admin opens the page from settings, types a program with an error and sees it underlined, fixes and saves it, sends a test Log and finds the new Attribute in the Logs explorer, restores the previous version; a member sees the page read-only. No screenshots.

## Answer

Built and merged (`8d232ef`, Restore fix `dc4bb02`, 2026-10-21). `LogTransformsController` (`show`, `update`, `check` proxying EventStore for any member, `restore` by version id within the Project), Inertia `LogTransform/Show` (editor with lint through `marksOf` byte-to-character spans, 500 ms debounce, Save/Restore confirms, running counts with a failures link, the saved version's compile error, the last 20 versions with View and Restore, read-only for members), and a "Log transform" card on the Project's settings. The shared `CodeEditor` takes `language: 'vrl'`, `readOnly`, `marks`, `minLines`. Decisions: a member's save is refused through the app's Pundit redirect, not a 403; a refused save renders the page again (VRL's errors can overflow the session cookie); the draft follows the current version id after a save or restore. Flow `e2e/flows/log-transform.spec.ts` passes on the dev stack (2 tests), as do `query-editor` and `project-keys`.
