# Unreleased

## Added

- **Resume an Agent session on another machine.** `solidtrace resume SESSION_ID [--fork] [--print] [-- CLAUDE_ARGS...]` downloads an Agent session from SolidTrace into the Claude Code project folder of this checkout, and starts `claude --resume`. Run it in a checkout of the repository of the Agent session. Your own Agent session continues with the same id, and its uploads continue. Another person's Agent session, a Run's, or yours with `--fork` continues as a new Agent session of yours, and the first Agent session does not change. The command tells you how to get the branch of the Agent session, and refuses with what to do in a checkout of another repository, with an old sign-in, or when the transcript is partial or cut. The Agent session page shows the command under **Resume on another machine**, with **Copy**. See [Resume an Agent session on another machine](../agent-sessions.md#resume-an-agent-session-on-another-machine).
  - The new Scope `sessions:download` downloads each Agent session of the Organization's Agent Project. `solidtrace login` asks for it, and you can put it on your own API token. A child token never has it. It gives no new access: each member reads these Agent sessions in Console.
  - Capture finds the start folder of a resumed Agent session on this machine. It sends nothing of an Agent session that is not yours, and `solidtrace capture status` shows it as "not yours".

## Changed

- **One Claude Code plugin.** The `solidtrace` plugin now has capture and the review skills, and the `solidtrace-skills` plugin goes. The skills have new names: `/solidtrace:code-review`, `/solidtrace:deep-review`, `/solidtrace:open-pr`, `/solidtrace:update-pr` and `/solidtrace:try-pr`. The release has one plugin zip, `solidtrace-plugin.zip`. The setup script installs the `solidtrace` plugin and removes `solidtrace-skills`. `SOLIDTRACE_REVIEW_PLUGIN=no` goes, because the skills are in the same plugin as capture. Records of earlier sessions stay as they are: the digest finds a review skill by the last part of its name, so `solidtrace-skills:code-review` and `solidtrace:code-review` are the same to it.

## Upgrading

Upgrade Console and EventStore first. Then each engineer runs the setup script again, and then `solidtrace login` again, so that the sign-in has `sessions:download`. A new CLI cannot sign in to an older Console, because the older Console does not know the Scope `sessions:download`. Until an engineer signs in again, `solidtrace resume` tells them to run `solidtrace login`, and capture works as before. A new CLI with an older EventStore captures as before too: the older EventStore does not tell whose an Agent session is, so capture sends it, and the EventStore refuses another person's bytes with 403, as before. `solidtrace resume` then tells the engineer to ask the operator to upgrade.

The setup script removes `solidtrace-skills@solidtrace`, so each skill does not show two times. Then restart Claude Code, and use the new names, such as `/solidtrace:open-pr`.
