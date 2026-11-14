# SolidTrace capture for Claude Code

This plugin uploads your Claude Code Agent sessions to SolidTrace, when the repository of an Agent session is in your Organization's captured repositories.

The plugin is a hooks file only. `Stop`, `SubagentStop` and `SessionEnd` upload the new bytes, and `SessionStart` sweeps the Agent sessions that a crash or a closed terminal left. Each hook runs `solidtrace capture --detach`, which starts the upload in the background and returns at once, so a hook never makes Claude Code wait, also with no network.

The plugin holds no instance URL and no secret. It needs:

1. The `solidtrace` CLI on your `PATH`, from the same SolidTrace release (its `install.sh`).
2. `solidtrace login`, once. Console's capture setup page gives the full command.

`solidtrace capture status` shows your Organization, its captured repositories, and each Agent session on this machine as uploaded, not in the list, or failed. A failure goes to `capture.log` in the solidtrace config directory.
