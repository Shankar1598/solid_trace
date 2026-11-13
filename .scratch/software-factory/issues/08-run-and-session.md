# 08: What are a Run and a Session?

Type: grilling
Status: open
Blocked by: 05, 07
Map: [SolidTrace Software Factory](../map.md)

## Question

What a Run is, what a Session is, and how they relate:

- An engineer's laptop session and a dispatched run on Nomad: the same thing, or a Run that holds one or more Sessions?
- Subagents, resumed and forked sessions: one Session or several?
- Where run state lives when no factory is connected, and which fields SolidTrace owns versus the dispatcher.
- How a Run joins to a PR, a release, an Issue or Monitor that started it, and its Evidence.
- Which Project a Session belongs to.

Cases from [How can a transcript be captured?](07-transcript-capture.md) (2026-11-13): is a fork its own Session (Codex records `forked_from_id`; Claude's CLI shows no parent field)? Is a `/clear` a new Session? Is a Codex subagent thread a Session, or part of its root's? Claude workflow agents sit under `subagents/workflows/wf_<id>/`.

Settle the glossary words with `domain-modeling`: Run, Session, and any of Factory, Automation, Dispatcher this ticket touches. Sessions are a fourth signal beside Issues, Logs and traces ([What is the factory, and who is it for?](01-what-the-factory-is-and-who-its-for.md)).
