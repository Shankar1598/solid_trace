# 11: How is a factory defined?

Type: grilling
Status: open
Blocked by: 05
Map: [SolidTrace Software Factory](../map.md)

## Question

A team's factory lives in a repository, synced by the dispatcher. What's in it?

- Nomad job files directly, or a format of our own that renders to them? Warp uses `factory.yaml` with Markdown agents (`~/.cache/solid-trace/software-factory-notes/warp.md` (local only)).
- Where agents, skills and prompts live. The candidate in [research/fable-opinion.md](../research/fable-opinion.md) is "in the harnesses' own files" (`CLAUDE.md`, `.claude/`, `AGENTS.md`).
- Where triggers, Scorers and secrets' names are declared.
- What a self-improvement PR changes in it.
