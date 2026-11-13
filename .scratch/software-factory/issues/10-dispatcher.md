# 10: What is the dispatcher, and how does it reach SolidTrace?

Type: grilling
Status: open
Blocked by: 05, 08, 11, 12
Map: [SolidTrace Software Factory](../map.md)

## Question

The dispatcher is one small service beside the Nomad server, holding triggers, the GitHub App, the factory repository sync, run state and secrets ([What is the factory, and who is it for?](01-what-the-factory-is-and-who-its-for.md)). Settle:

- Its language and its database.
- How it signs in to SolidTrace, and how users sign in to it through SolidTrace.
- How Console reads run state from it, and what it writes into SolidTrace (over public ingest and MCP only).
- The one small interface over Nomad: dispatch a run, watch its state, store a secret.
- Its API for starting, cancelling and approving runs, used by GitHub, Slack and scripts.

Input from [How can a run be isolated on Nomad?](09-run-isolation-on-nomad.md) (2026-11-13, [research/nomad-run-isolation.md](../research/nomad-run-isolation.md)):

- Do the token broker (one-hour, single-repo forge tokens, checked against the run's Nomad identity) and the model gateway live in the dispatcher, or apart from it?
- Nomad Variables can't hold per-run secrets: every concurrent dispatch of a job reads the same paths. Per-run secrets come from the broker instead.
- The dispatcher owns retries (restart and reschedule set to 0) and records timeouts from the allocation's description, since a timed-out run reads as `complete`.
- Unverified: whether a poststop task runs after `nomad job stop`, so a cancelled run may never push. It needs a test on a real cluster before this ticket resolves (a task, if it blocks).
