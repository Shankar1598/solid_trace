# 07: Preview on the page

**What to build:** On the Log transform page, anyone in the Organization previews a draft on the Project's recent arrivals or on Logs they paste in, and sees each Log before and after side by side, with how many were kept, dropped and failed. Nothing is stored. See the spec's "The page" and user stories 12 to 15 and 26.

**Blocked by:** 06

**Status:** ready-for-agent

- [ ] Preview runs the editor's draft through the preview route on the recent arrivals, or on pasted JSON Logs.
- [ ] Each Log's before and after side by side; dropped and failed Logs say so, with the error.
- [ ] Kept, dropped and failed counts are shown.
- [ ] A draft that doesn't compile shows the check's errors instead.
- [ ] Members can preview.
- [ ] The Playwright flow from 06 previews the fixed program before saving it.
