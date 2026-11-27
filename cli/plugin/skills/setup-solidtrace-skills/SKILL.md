---
name: setup-solidtrace-skills
description: Install the pstack plugin and set up its models, and install the /wait-what and deslop skills, for this repository or for this user.
disable-model-invocation: true
---

# Set up the SolidTrace skills

The skills of this plugin need no other skills. When they are installed, `open-pr` cleans up the branch with the `deslop` skill before its final review, and `open-pr` and `update-pr` write the PR body with the `/wait-what` skill and pstack's `technical-writing` skill. This skill installs pstack, sets up its models, and installs `/wait-what` and `deslop`.

Do the steps in order. When a step finds its part already there, it changes nothing.

## 1. Choose the scope

`<root>` is `git rev-parse --show-toplevel`. Run every command of this skill from `<root>`, or from this folder when it is not in a Git repository.

- When the arguments say `repo` or `global` (`$ARGUMENTS`), use that scope.
- When this folder is not in a Git repository, the scope is `global`.
- Else ask the person with `AskUserQuestion`, with the options "This repository (Recommended)" and "Global, for this user". When you cannot ask, the scope is `repo`.

The scope sets where each part goes:

| Part | `repo` | `global` |
|---|---|---|
| pstack | `--scope project`: `<root>/.claude/settings.json` | `--scope user` |
| The model rule `<rule>` | `<root>/.claude/rules/pstack-models.md` | `~/.claude/rules/pstack-models.md` |
| A skill `<name>` of step 4 | `<root>/.claude/skills/<name>`, and `<root>/skills-lock.json` | `~/.claude/skills/<name>` |

Done when you hold the scope.

## 2. The pstack plugin

`<s>` is the scope that the table gives. Run `claude plugin marketplace add cursor/plugins --scope <s> --json`. It declares the `cursor-plugins` marketplace in the settings of the scope, so a teammate who clones the repository gets pstack too. When the marketplace is already there, it only declares it. When an `outcome` of this step is not `ok`, give the person the `message` and stop.

Run `claude plugin list --json`. pstack is there when an entry has an `id` that starts with `pstack@`, and its `scope` is `project` with `projectPath` equal to `<root>` (for `repo`), or its `scope` is `user` (for `global`).

- No such entry: run `claude plugin install pstack@cursor-plugins --scope <s> --json`.
- The entry has `"enabled": false`: run `claude plugin enable <id> --scope <s>`.

Run `claude plugin list --json` again. The entry's `installPath` is `<pstack>`. A plugin that you installed loads only in a new session, but its files are in `<pstack>` now.

Done when the list shows the entry with `"enabled": true`, and you hold `<pstack>`.

## 3. The pstack models

When `<rule>` has the line `# defaults: 3`, the setup is done. Tell the person that `/pstack:setup-pstack` changes it, and go to step 4.

Else read `<pstack>/docs/harness/claude-code.md`, then `<pstack>/skills/setup-pstack/SKILL.md`, and follow them, with `<rule>` in place of `~/.claude/rules/pstack-models.md`. When `<rule>` does not exist but the rule file of the other scope does, its lines are the current choices. The Skill tool does not run that skill. Claude Code loads the `.md` files of `<root>/.claude/rules/` and of `~/.claude/rules/` into each session.

Done when `<rule>` has the line `# defaults: 3`.

## 4. The /wait-what and deslop skills

| `<name>` | `<source>` |
|---|---|
| `wait-what` | `mattpocock/skills` |
| `deslop` | `cursor/plugins` (from its `cursor-team-kit` plugin) |

A skill is there when `<scope folder>/SKILL.md` exists, with the folder from the table of step 1. A plugin that has the skill does not count, because it gives the skill only with the plugin's prefix, such as `/mattpocock-skills:wait-what`.

For each skill that is not there, run from `<root>`:

- `repo`: `npx -y skills add <source> --skill <name> --agent claude-code --yes`
- `global`: `npx -y skills add <source> --skill <name> --agent claude-code --global --yes`

For `cursor/plugins`, the command can warn that it skipped another skill of the repository. That warning is not a failure. When `npx` is not on the `PATH`, tell the person to install Node.js and run this skill again.

Done when `<scope folder>/SKILL.md` exists for both skills.

## 5. Report

Write one line for each part (pstack, its models, `/wait-what`, `deslop`): already there, installed, or set up, or the failure. Then:

- When you installed pstack or a skill, tell the person to start a new Claude Code session to use them.
- For `repo`, list the files that changed in `<root>` (`git status --short`), for the person to commit.
- When both `<root>/.claude/rules/pstack-models.md` and `~/.claude/rules/pstack-models.md` exist, tell the person that each session loads both.
