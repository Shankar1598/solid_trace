# 12: Where do OAuth, scoped tokens, SSO and an audit log fall in the order?

Type: grilling
Status: open
Blocked by: 05
Map: [SolidTrace Software Factory](../map.md)

## Question

OAuth for the MCP server is the planned next effort (`.scratch/STATUS.md`). The factory needs more:

- The factory signs users in through SolidTrace (OAuth).
- Machine tokens for runs and laptops: scoped to a Project and an action (ingest only, MCP read), and expiring. Today API tokens have no scopes or expiry (`docs/mcp.md`).
- SSO and an audit log, for larger teams. SolidTrace has neither.

Which of these land before the record stage, which before the run stage, and which are separate efforts? The candidate is that hardening is part of the product and lands before the run stage ships to a team ([research/fable-opinion.md](../research/fable-opinion.md), section 3).
