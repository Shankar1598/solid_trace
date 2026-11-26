# Unreleased

## Changed

- **One Claude Code plugin.** The `solidtrace` plugin now has capture and the review skills, and the `solidtrace-skills` plugin goes. The skills have new names: `/solidtrace:code-review`, `/solidtrace:deep-review`, `/solidtrace:open-pr`, `/solidtrace:update-pr` and `/solidtrace:try-pr`. The setup script installs the `solidtrace` plugin and removes `solidtrace-skills`. `SOLIDTRACE_REVIEW_PLUGIN=no` goes, because the skills are in the same plugin as capture. Records of earlier sessions stay as they are: the digest finds a review skill by the last part of its name, so `solidtrace-skills:code-review` and `solidtrace:code-review` are the same to it.
- **The marketplace is in the repository.** Claude Code adds the SolidTrace marketplace from GitHub at the release's tag: `claude plugin marketplace add Shankar1598/solid_trace#<tag> --sparse .claude-plugin cli/plugin`. It clones only those two folders. The release no longer has `marketplace.json` or a plugin zip. Capture setup's commands and managed-settings snippet give the `github` source at the instance's release. The release commit sets the version in `cli/plugin/.claude-plugin/plugin.json`, and the release job fails when it is not the tag's version.

## Upgrading

Each engineer runs the setup script again. It removes the old `solidtrace` marketplace (the downloaded `marketplace.json`) with its plugins, also `solidtrace-skills@solidtrace`, so each skill does not show two times. It then adds the marketplace from GitHub and installs `solidtrace@solidtrace`. You can delete `~/.local/share/solidtrace/.claude-plugin/`. Then restart Claude Code, and use the new names, such as `/solidtrace:open-pr`.

If your managed settings have the `solidtrace` marketplace with a `url` source, change it to the new snippet on the Capture setup page.
