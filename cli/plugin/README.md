# SolidTrace for Claude Code

This plugin uploads your Claude Code Agent sessions to SolidTrace, when the repository of an Agent session is in your Organization's captured repositories. It also gives the review skills: `code-review`, `deep-review`, `open-pr`, `update-pr` and `try-pr` ([Agent sessions](../../docs/agent-sessions.md#record-what-each-review-found-the-review-skills)). `setup-solidtrace-skills` installs the pstack plugin, sets up its models, and installs `/wait-what` from `mattpocock/skills` and `deslop` from `cursor/plugins`, when they are not there, for this repository (the default) or for this user. `beta-pr-review-canvas` (beta) makes an HTML page that walks a reviewer through a Bitbucket or GitHub PR, built on the PR body that `open-pr` writes. It is a copy of `pr-review-canvas` from Cursor's `cursor-team-kit` (MIT).

The capture part is the hooks file. `Stop`, `SubagentStop` and `SessionEnd` upload the new bytes, and `SessionStart` sweeps the Agent sessions that a crash or a closed terminal left. Each hook runs `solidtrace capture --detach`, which starts the upload in the background and returns at once, so a hook never makes Claude Code wait, also with no network.

The plugin holds no instance URL and no secret. It needs:

1. The `solidtrace` CLI on your `PATH`, from the same SolidTrace release (its `install.sh`).
2. `solidtrace login`, once. Console's capture setup page gives the full command.

To install the plugin, add the marketplace at the repository root (`.claude-plugin/marketplace.json`) at your release's tag. Claude Code clones only the `.claude-plugin` and `cli/plugin` folders. Console's capture setup page gives the commands with your instance's release:

```bash
claude plugin marketplace add Shankar1598/solid_trace#<tag> --sparse .claude-plugin cli/plugin
claude plugin install solidtrace@solidtrace
claude plugin list
```

`claude plugin list` must show `solidtrace@solidtrace` as enabled.

`solidtrace capture status` tells you if capture works on this machine: it checks the sign-in, this plugin, the captured repositories list and the uploads, and counts the Agent sessions on this machine as uploaded, not captured, failed or not checked yet. `--all` lists each Agent session. A failure goes to `capture.log` in the solidtrace config directory.
