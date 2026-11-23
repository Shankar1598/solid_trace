# SolidTrace capture for Claude Code

This plugin uploads your Claude Code Agent sessions to SolidTrace, when the repository of an Agent session is in your Organization's captured repositories.

The plugin is a hooks file only. `Stop`, `SubagentStop` and `SessionEnd` upload the new bytes, and `SessionStart` sweeps the Agent sessions that a crash or a closed terminal left. Each hook runs `solidtrace capture --detach`, which starts the upload in the background and returns at once, so a hook never makes Claude Code wait, also with no network.

The plugin holds no instance URL and no secret. It needs:

1. The `solidtrace` CLI on your `PATH`, from the same SolidTrace release (its `install.sh`).
2. `solidtrace login`, once. Console's capture setup page gives the full command.

To install the plugin, download the release's `marketplace.json` into a `.claude-plugin` folder, and add it from there. Claude Code reads a `github.com` URL as a git repository, and it does not load a marketplace file that you add as a plain path. Console's capture setup page gives the commands with your instance's release:

```bash
mkdir -p ~/.local/share/solidtrace/.claude-plugin
curl -fsSL -o ~/.local/share/solidtrace/.claude-plugin/marketplace.json https://github.com/Shankar1598/solid_trace/releases/download/<tag>/marketplace.json
claude plugin marketplace add ~/.local/share/solidtrace/.claude-plugin/marketplace.json
claude plugin install solidtrace@solidtrace
claude plugin list
```

`claude plugin list` must show `solidtrace@solidtrace` as enabled.

`solidtrace capture status` tells you if capture works on this machine: it checks the sign-in, this plugin, the captured repositories list and the uploads, and counts the Agent sessions on this machine as uploaded, not captured, failed or not checked yet. `--all` lists each Agent session. A failure goes to `capture.log` in the solidtrace config directory.
