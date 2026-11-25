---
name: try-pr
description: Get the code of a PR or a branch into a worktree of its own, start the repository's dev stack from it, and give the URLs and the sign-in. With `done <PR>`, stop that stack and remove the worktree. With `list`, show each one.
disable-model-invocation: true
---

# Try PR

The person typed this command to use the code of a PR in the app on this machine, before they approve the PR. The PR can be an agent's PR, another person's PR, or an old PR of their own. Do the steps without asking, except where a step says to ask.

The arguments are `$ARGUMENTS`:

- `<PR>`: a GitHub or Bitbucket Cloud PR link, a PR number of this clone's `origin`, or a branch name. For a branch, the script uses this clone's branch when it has commits that origin's branch does not have, else origin's. Give a branch name of only digits as `branch:<name>`. Do steps 1 to 4.
- `done <PR>`: do Done.
- `list`: do List.

The script `"${CLAUDE_SKILL_DIR}/try-pr.sh"` does the git and forge work. Each Bash call is a new shell, so write that full path in each command, as the steps below do. Run it from the session's folder, which is in the person's clone. When it stops with a `try-pr:` line, give that line to the person and stop. It refuses a closed or merged PR, a Bitbucket PR from a fork, and a worktree that has changes or commits that no branch has. For commits, its line gives the `git branch` command that keeps them. It needs bash, git, `gh` for GitHub, and `curl` and `jq` for Bitbucket Cloud. A repository's own recipe can need more: SolidTrace's `verify-solidtrace` runs on Linux only, so SolidTrace PRs run on the dev VM, not on a Mac.

## 1. Get the code

Run `"${CLAUDE_SKILL_DIR}/try-pr.sh" fetch <PR>`. It makes the worktree `<clone>/.worktrees/try-pr-<n>` for a PR, or `<clone>/.worktrees/try-pr-<branch>-<hash>` for a branch, detached at the head, or it moves the same worktree to the newest head. For a branch, `<branch>` is the name with `-` in place of `/` and other such characters, and `<hash>` is a short hash of the full name, so `feature/x` and `feature-x` get a worktree each. The person's own checkout and branches do not change. When the clone does not ignore `.worktrees/`, the script adds `/.worktrees/` to `.git/info/exclude` and prints an `ignored` line.

It prints the lines `worktree`, `pr`, `title`, `author`, `branch`, `head` and `behind`. Keep them for steps 3 and 4. When this clone's branch runs, the `branch` line says so, and why.

When it refuses with "a stack runs from", the stack that this skill started from the old head is up. Stop it as Done says in its step 2, then run fetch again.

Before step 2, show the person the PR's author: the `author` line, or for a branch the head commit's author (`git -C <worktree> log -1 --format='%an <%ae>'`). The author is the person when the forge account is theirs (GitHub: `gh api user --jq .login`). Where you cannot compare accounts (Bitbucket, a branch), compare with the person's git name and email (`git config user.name`, `git config user.email`). When the author is not the person, or you cannot tell, tell the person that the PR's setup and launch scripts run as them, with their files and credentials. When the worktree has its own launch skill (step 2, kind 1), you also follow that skill's text, which is the PR's text. Ask the person whether to start the stack. On a no, stop, and tell them that `done <PR>` removes the worktree.

Done when: the script printed the `worktree` line, and the author is the person or the person said yes.

## 2. Find the launch recipe

try-pr knows no repository: each repository says how its dev stack starts. Use the first of these that you find:

1. **The worktree's launch skill**: a skill in the worktree's `.claude/skills/` whose description says that it starts or launches the app or a stack. For SolidTrace, it is `verify-solidtrace`, with `verify up`.
2. **The clone's launch skill**: such a skill in this session's checkout, when the PR's head is older than the skill. Copy it into the worktree with `"${CLAUDE_SKILL_DIR}/try-pr.sh" copy <PR> .claude/skills/<name>`, and use the copy in the worktree. The skill's helper finds its checkout from its own folder, so the copy runs the clone's recipe on the PR's code. fetch and done delete the copy.
3. **The docs**: the worktree's `CLAUDE.md`, `AGENTS.md` and README.

Read the recipe fully.

The stack must use ports and data of its own, and leave the person's processes and ports as they are. The person's own dev stack can be up. A launch skill (1 or 2) owns that. With the docs (3), pick ports that are free (`lsof -nP -iTCP:<port> -sTCP:LISTEN` shows nothing), and a database name and a data folder for the worktree only. Say which ones you picked.

**Never run a start script that kills processes, or that holds fixed ports that can be in use.** For SolidTrace, these are `bin/dev`, `bin/clear-ports` and `bin/clickhouse-dev`. When such a script is the only recipe, stop and tell the person why.

Done when: you can name the command that starts the stack, the command that stops it, and the command that tells if it runs.

## 3. Start the stack

1. Do the setup that the recipe gives for a new checkout, such as the package install. A new worktree has no packages and no build output. A PR that changes the dependencies (for example `Gemfile.lock`) can need `bundle install` or the repository's install step, also when the recipe does not say so.
2. Start the stack so that it continues after this session ends, as the recipe does it. Wait for the recipe's ready signal, or until each URL answers.
3. Record the stack: give the stack note on stdin to `"${CLAUDE_SKILL_DIR}/try-pr.sh" started <PR>`. The note records that this skill started this stack. fetch and done refuse the worktree while it is there, and List shows it.

   ```text
   recipe: <the skill or the doc that you followed, and "copied from the clone" for kind 2>
   urls: <each URL, with its port>
   status: <a command that exits 0 only while the stack runs>
   stop: <the command that stops the stack>
   ```

   The `status` and `stop` lines must name the one stack that you started, with absolute paths: a command that acts on "the newest stack" can act on another stack. For verify-solidtrace, use the worktree's helper and the run directory that `verify up` printed:

   ```text
   status: <worktree>/.claude/skills/verify-solidtrace/bin/verify list | grep -q '^up   <run directory name> '
   stop: <worktree>/.claude/skills/verify-solidtrace/bin/verify down --run <run directory>
   ```

When a step fails, stop what started with the recipe's own stop, and record no note. Tell the person which step failed, with the last lines of its output, and the `behind` line: an old PR can need an old setup, and a copied recipe (kind 2) is newer than the PR's code. Run the PR's head as it is. The person decides about a merge or a rebase.

Done when: each URL answers and `"${CLAUDE_SKILL_DIR}/try-pr.sh" list` shows the note, or the person has the failure.

## 4. Tell the person

Give these, in this order:

- The PR: its title, author and link, the head commit, and the `behind` line.
- The worktree's path, and the recipe: the worktree's own, copied from the clone, or the docs.
- Each URL with its port, and the sign-in: the user and the password that the recipe seeds.
- On Linux, the host name (`hostname`). The person can be on a laptop with a port forward to this machine, so name each port to forward.
- After a start from the docs (kind 3): a launch skill in the repository makes the next start repeatable.
- The stop: `done <PR>`, with the command name that the person typed: `/try-pr done <PR>` when the skill is linked into `~/.claude/skills/try-pr`, or `/solidtrace-skills:try-pr done <PR>` from the solidtrace-skills plugin.

## Done

1. Run `"${CLAUDE_SKILL_DIR}/try-pr.sh" list`, and find the PR's worktree. With no note, this skill started no stack there: go to step 3.
2. Stop the stack with the `stop` line of the note. Stop only the stack of the note: another stack on this machine is not this skill's to stop. Run the `status` line. When it says that the stack does not run, run `"${CLAUDE_SKILL_DIR}/try-pr.sh" stopped <PR>`, which deletes the note.
3. Run `"${CLAUDE_SKILL_DIR}/try-pr.sh" done <PR>`. It deletes the copies of step 2, then removes the worktree. When git refuses because the worktree has changes, show the person the changes (`git -C <worktree> status`) and ask whether to delete them. On a yes, run `git worktree remove --force <worktree>`.

Done when: `git worktree list` does not show the worktree, and no stack of the note runs.

## List

Run `"${CLAUDE_SKILL_DIR}/try-pr.sh" list`. For each worktree with a note, run the note's `status` line. Give each worktree's path, its head, its copies, and whether its stack runs, with its URLs.
