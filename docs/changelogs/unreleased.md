# Unreleased

The notes of the next release, for the changes since v0.25.0 on this file. At the tag, rename this file to `<tag>.md`, add the notes of the other changes, and add its Release record.

## Added

- **`/solidtrace:beta-pr-review-canvas` (beta).** It makes an HTML page that walks a reviewer through a Bitbucket or GitHub PR, or the PR file of `open-pr local`: the summary and the risks of the PR body first, the core files open with notes, and the files to skim collapsed. Code that moved shows in blue or purple. It serves the page on `127.0.0.1:8432`. It is a copy of `pr-review-canvas` from Cursor's `cursor-team-kit` (MIT).
