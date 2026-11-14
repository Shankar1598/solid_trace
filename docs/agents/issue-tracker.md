# Issue tracker: Local Markdown

Issues and specs for this repo live as markdown files in `.scratch/`.

## Where `.scratch/` lives

- `.scratch/` is a private Git repository of its own. It is cloned into the root of the main checkout. This repo ignores it, because this repo is public.
- Never add a `.scratch/` file to this repo.
- To commit tracker changes, run Git inside `.scratch/`: `git -C .scratch add -A && git -C .scratch commit -m "<message>" && git -C .scratch push`.
- A cron job commits and pushes `.scratch/` every 15 minutes, so the data stays durable when a session forgets to push.
- Worktrees do not have `.scratch/`. An agent in a worktree reads tickets from the main checkout, at the absolute path, or from the text in its brief. Only a session in the main checkout commits to the tracker.
- On a new machine, clone the private tracker repository into `.scratch/`.

## Conventions

- One feature per directory: `.scratch/<feature-slug>/`
- The spec is `.scratch/<feature-slug>/spec.md`
- Implementation issues are one file per ticket at `.scratch/<feature-slug>/issues/<NN>-<slug>.md`, numbered from `01`, never a single combined tickets file
- Triage state is recorded as a `Status:` line near the top of each issue file (see `triage-labels.md` for the role strings)
- Comments and conversation history append to the bottom of the file under a `## Comments` heading
- Project status is `.scratch/STATUS.md`: per-effort counts, the next ticket, and a last-session note. It links out; it does not duplicate ticket bodies.

## Project status

Update `.scratch/STATUS.md` when a ticket's `Status:` line changes, and again at the end of a session (Last session + Next). Done means `resolved` or `wontfix`. Open means every other `Status:` value (`open`, `claimed`, `ready-for-agent`, `ready-for-human`, `needs-triage`, `needs-info`).

## When a skill says "publish to the issue tracker"

Create a new file under `.scratch/<feature-slug>/` (creating the directory if needed).

## When a skill says "fetch the relevant ticket"

Read the file at the referenced path. The user will normally pass the path or the issue number directly.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a file with one **child** file per ticket.

- **Map**: `.scratch/<effort>/map.md` (the Notes / Decisions-so-far / Fog body).
- **Child ticket**: `.scratch/<effort>/issues/NN-<slug>.md`, numbered from `01`, with the question in the body. A `Type:` line records the ticket type (`research`/`prototype`/`grilling`/`task`); a `Status:` line records `claimed`/`resolved`.
- **Blocking**: a `Blocked by: NN, NN` line near the top. A ticket is unblocked when every file it lists is `resolved`.
- **Frontier**: scan `.scratch/<effort>/issues/` for files that are open, unblocked, and unclaimed; first by number wins.
- **Claim**: set `Status: claimed` and save before any work.
- **Resolve**: append the answer under an `## Answer` heading, set `Status: resolved`, then append a context pointer (gist + link) to the map's Decisions-so-far in `map.md`.
