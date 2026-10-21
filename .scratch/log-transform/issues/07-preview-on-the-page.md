# 07: Preview on the page

**What to build:** On the Log transform page, anyone in the Organization previews a draft on the Project's recent arrivals or on Logs they paste in, and sees each Log before and after side by side, with how many were kept, dropped and failed. Nothing is stored. See the spec's "The page" and user stories 12 to 15 and 26.

**Blocked by:** 06

**Status:** resolved

- [ ] Preview runs the editor's draft through the preview route on the recent arrivals, or on pasted JSON Logs.
- [ ] Each Log's before and after side by side; dropped and failed Logs say so, with the error.
- [ ] Kept, dropped and failed counts are shown.
- [ ] A draft that doesn't compile shows the check's errors instead.
- [ ] Members can preview.
- [ ] The Playwright flow from 06 previews the fixed program before saving it.

## Answer

Built and merged (`acf51e5`, 2026-10-21). `LogTransformsController#preview` (any member; pasted Logs read from the raw JSON body so `null`s survive), `lib/logTransform/preview.ts` (`pastedLogs`), and `DraftPreview` on the page: recent arrivals or pasted Logs, kept/dropped/failed counts, each Log's before and after as the inspector's JSON, the first 50 with "Show N more", and a note when the draft changed since the preview. Decision (orchestrator): members edit and preview drafts (story 26 over the spec's "read-only" line) but get no Save or Restore. Flows pass on the dev stack.
