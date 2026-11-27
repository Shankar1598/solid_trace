# Unreleased

## Added

- **Evidence files open in a popup.** On the Agent session page and the Review record page, a click on an Evidence file opens it in a popup on the same page: the image fit to the window, or the video with its speed control, the whole caption, "n of N" with the previous and the next file (the buttons, or ← and →), and **Open page**. Esc closes it. A click with Ctrl, Cmd, Shift or the middle button still opens the file's page.
- **The PR body on the Review record page.** `solidtrace review record --pr-body FILE` keeps the PR body in the record's new `pr_body` (the version stays 2), and refuses a body of more than 65,536 characters, GitHub's limit. `open-pr` and `update-pr` give it the body that they wrote. The page shows it as Markdown before the Story, with the rules of a Text block, and `get_review_record` gives it. A record from plugin 0.24 or earlier has no PR body, and the page says so.

## Changed

- **One Claude Code plugin.** The `solidtrace` plugin now has capture and the review skills, and the `solidtrace-skills` plugin goes. The skills have new names: `/solidtrace:code-review`, `/solidtrace:deep-review`, `/solidtrace:open-pr`, `/solidtrace:update-pr` and `/solidtrace:try-pr`. The setup script installs the `solidtrace` plugin and removes `solidtrace-skills`. `SOLIDTRACE_REVIEW_PLUGIN=no` goes, because the skills are in the same plugin as capture. Records of earlier sessions stay as they are: the digest finds a review skill by the last part of its name, so `solidtrace-skills:code-review` and `solidtrace:code-review` are the same to it.
- **The marketplace is in the repository.** Claude Code adds the SolidTrace marketplace from GitHub at the release's tag: `claude plugin marketplace add Shankar1598/solid_trace#<tag> --sparse .claude-plugin cli/plugin`. It clones only those two folders. The release no longer has `marketplace.json` or a plugin zip. Capture setup's commands and managed-settings snippet give the `github` source at the instance's release. The release commit sets the version in `cli/plugin/.claude-plugin/plugin.json`, and the release job fails when it is not the tag's version.

## Fixed

- **No empty runs in a Review record.** A findings block that cannot be read (a block that Claude Code's output limit cut, or a block that came back in a subagent's report) no longer makes a review run with no skill, stage, reviewers or verifier. `solidtrace review record` warns that the record has none of its findings, and a review start that only such a block follows is a run of its own, with the start's skill.
- **The Review record page.** A run that a record already has with no skill, stage, reviewers or verifier shows one muted line: "No data: the findings block of this run could not be read." The Details of a finding wrap long paths, so they stay in their cell.

## Upgrading

Each engineer runs the setup script again. It removes the old `solidtrace` marketplace (the downloaded `marketplace.json`) with its plugins, also `solidtrace-skills@solidtrace`, so each skill does not show two times. It then adds the marketplace from GitHub and installs `solidtrace@solidtrace`. You can delete `~/.local/share/solidtrace/.claude-plugin/`. Then restart Claude Code, and use the new names, such as `/solidtrace:open-pr`.

If your managed settings have the `solidtrace` marketplace with a `url` source, change it to the new snippet on the Capture setup page.
