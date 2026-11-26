# Unreleased

## Changed

- **One Claude Code plugin.** The `solidtrace` plugin now has capture and the review skills, and the `solidtrace-skills` plugin goes. The skills have new names: `/solidtrace:code-review`, `/solidtrace:deep-review`, `/solidtrace:open-pr`, `/solidtrace:update-pr` and `/solidtrace:try-pr`. The release has one plugin zip, `solidtrace-plugin.zip`. The setup script installs the `solidtrace` plugin and removes `solidtrace-skills`. `SOLIDTRACE_REVIEW_PLUGIN=no` goes, because the skills are in the same plugin as capture. Records of earlier sessions stay as they are: the digest finds a review skill by the last part of its name, so `solidtrace-skills:code-review` and `solidtrace:code-review` are the same to it.

## Upgrading

Each engineer runs the setup script again. It removes `solidtrace-skills@solidtrace`, so each skill does not show two times. Then restart Claude Code, and use the new names, such as `/solidtrace:open-pr`.
