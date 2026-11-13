# 16: Write the spec and first-milestone tickets

Type: task
Status: open
Blocked by: 04, 05, 06, 07, 08, 09, 10, 11, 12, 13, 17
Map: [SolidTrace Software Factory](../map.md)

## Question

When the decision tickets are resolved, write the factory spec and the first milestone's implementation tickets in a new `.scratch/` effort folder, as the logs map did with `logs-v1`. Every decision in the spec links its resolved ticket. Write ADRs for the hard-to-reverse ones, at least: agents run on Nomad, never the SolidTrace instance, with the factory as a separable module in this repo ([Where do agents run, and where does the factory's code live?](02-where-agents-run-and-where-the-code-lives.md)); and how transcripts are stored ([Where are transcripts stored?](06-transcript-storage.md)).

Fog that graduates into tickets before this one is reached adds them to its blockers.
