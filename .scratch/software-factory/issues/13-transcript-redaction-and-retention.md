# 13: How are transcripts redacted and retained?

Type: grilling
Status: open
Blocked by: 06, 07
Map: [SolidTrace Software Factory](../map.md)

## Question

A transcript holds whatever the agent read: secrets, customer data, production Logs pulled over MCP. Settle:

- Where redaction happens: at the agent (content gates), on the server (the Log transform, the transcript parser), or both ([research/agent-transcripts.md](../research/agent-transcripts.md)).
- What's redacted by default, and whether a team can turn content capture off and keep metadata only.
- Redaction must cover Claude's `tool-results/` files and the request records in its transcripts, the most sensitive parts ([How can a transcript be captured?](07-transcript-capture.md)). Codex already cuts persisted command and MCP output at 64 KiB.
- Retention for transcripts, and whether it's separate from the Project's Retention.
- Who can read a transcript: the same grants as Logs, or narrower?
- The storage load on the production instance: about half a gigabyte a week per heavy user, so about 100 GB a month for fifty engineers, against production's 6.7 TiB.
