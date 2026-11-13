# 17: What must a run be able to do while it runs?

Type: grilling
Status: open
Blocked by: 04
Map: [SolidTrace Software Factory](../map.md)

## Question

[How can a run be isolated on Nomad?](09-run-isolation-on-nomad.md) gives the agent task no forge credential and pushes once, at the end ([research/nomad-run-isolation.md](../research/nomad-run-isolation.md)). That holds only if a run never needs more. Using the first team's answers:

- Must a run push mid-run, open a draft PR, or read CI results while it works? If so, the agent task holds a write token after all, and the forge's branch rules do the restricting.
- Do runs need Docker for their tests (testcontainers, compose)? That pushes towards privileged containers or Docker inside gVisor, which the research did not check.
- Which hosts must a run reach beyond the forge, the model and SolidTrace: package registries, a staging environment, production over MCP?
