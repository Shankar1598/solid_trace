---
name: try-pr
description: Get the code of a PR or a branch into a worktree of its own, start the repository's dev stack from it, and give the URLs and the sign-in. With `done <PR>`, stop that stack and remove the worktree. With `list`, show each one.
disable-model-invocation: true
---

# Try PR

The person typed this command to use the code of a PR in the app on this machine, before they approve the PR. The PR can be an agent's PR, another person's PR, or an old PR of their own. Do the steps without asking, except where a step says to ask.

The arguments are `$ARGUMENTS`:

- `<PR>`: a GitHub or Bitbucket Cloud PR link, a PR number of this clone's `origin`, or a branch name of `origin`. Do steps 1 to 4.
- `done <PR>`: do Done.
- `list`: do List.

The script `${CLAUDE_SKILL_DIR}/try-pr.sh` does the git and forge work. It is `S` below. Run it from the session's folder, which is in the person's clone. When it stops with a `try-pr:` line, give that line to the person and stop. It refuses a closed or merged PR, a Bitbucket PR from a fork, and a worktree that has changes.

## 1. Get the code

Run `"$S" fetch <PR>`. It makes the worktree `<clone>/.worktrees/try-pr-<n>`, detached at the PR's head, or it moves the same worktree to the newest head. The person's own checkout and branches do not change. When the clone does not ignore `.worktrees/`, the script adds `/.worktrees/` to `.git/info/exclude` and prints an `ignored` line.

It prints the lines `worktree`, `pr`, `title`, `author`, `branch`, `head`, `behind` and `note`. Keep them for steps 3 and 4.

When it refuses with "a stack runs from", the stack that this skill started from the old head is up. Stop it as Done says in its step 2, then run fetch again.

Done when: the script printed the `worktree` line.

## 2. Find the launch recipe

try-pr knows no repository: each repository says how its dev stack starts. Look in the worktree, in this order:

1. A project skill that launches the app: a skill in `.claude/skills/` whose description says that it starts or launches the app or a stack. For SolidTrace, it is `verify-solidtrace`, with `verify up`. Read its SKILL.md fully.
2. Else the worktree's `CLAUDE.md`, `AGENTS.md` and README. Read how the dev stack starts.

The stack must use ports and data of its own. The person's own dev stack can be up, and it keeps its ports and its data. A launch skill (kind 1) owns that. With the docs (kind 2), pick ports that are free (`lsof -nP -iTCP:<port> -sTCP:LISTEN` shows nothing), and a database name and a data folder for the worktree only. Say which ones you picked.

Done when: you can name the command that starts the stack, the command that stops it, and the command that tells if it runs.

## 3. Start the stack

1. Do the setup that the recipe gives for a new checkout, such as the package install. A new worktree has no packages and no build output.
2. Start the stack so that it continues after this session ends, as the recipe does it. Wait for the recipe's ready signal, or until each URL answers.
3. Write the note file, at the path of the `note` line. It records that this skill started this stack. fetch, Done and List read it.

   ```text
   recipe: <the skill or the doc that you followed>
   urls: <each URL, with its port>
   status: <the command that tells if the stack runs>
   stop: <the command that stops the stack>
   ```

When a step fails, stop what started with the recipe's own stop, and do not write the note. Tell the person which step failed, with the last lines of its output, and the `behind` line: an old PR can need an old setup. Run the PR's head as it is. The person decides about a merge or a rebase.

Done when: each URL answers and the note file is there, or the person has the failure.

## 4. Tell the person

Give these, in this order:

- The PR: its title, author and link, the head commit, and the `behind` line.
- The worktree's path.
- Each URL with its port, and the sign-in: the user and the password that the recipe seeds.
- On Linux, the host name (`hostname`). The person can be on a laptop with a port forward to this machine, so name each port to forward.
- After a start from the docs (kind 2): a launch skill in the repository makes the next start repeatable.
- The stop: `/solidtrace-skills:try-pr done <PR>`.

## Done

1. Run `"$S" list`, and find the PR's worktree. With no note, this skill started no stack there: go to step 3.
2. Stop the stack with the `stop` line of the note. Stop only the stack of the note: another stack on this machine is not this skill's to stop. Run the `status` line. When it says that the stack does not run, delete the note.
3. Run `"$S" done <PR>`. It removes the worktree. When git refuses because the worktree has changes, show the person the changes (`git -C <worktree> status`) and ask whether to delete them. On a yes, run `git worktree remove --force <worktree>`.

Done when: `git worktree list` does not show the worktree, and no stack of the note runs.

## List

Run `"$S" list`. For each worktree with a note, run the note's `status` line. Give each worktree's path, its head, and whether its stack runs, with its URLs.
