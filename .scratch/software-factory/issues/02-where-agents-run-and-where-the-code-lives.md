# 02: Where do agents run, and where does the factory's code live?

Type: grilling
Status: resolved
Map: [SolidTrace Software Factory](../map.md)

## Question

Where do the factory's agents run: on the SolidTrace instance, on VMs, or on a cluster? What schedules them? Does the factory live in this repository or its own? (The first map's Fog 4, VMs and isolation, in part, and Fog 6, Data, in part.)

## Answer

Resolved 2026-11-13 (grilling, in the session written up in [research/fable-opinion.md](../research/fable-opinion.md), section 1).

**Agents never run on the SolidTrace instance.**

> "The solid trace instance will be busy ingesting and processing logs from prod. we cannot run its harness in the same instance. we will need to run this on a cluster of pods."

**One repository, and Nomad.**

> "we should put the factory module into this repo itself and use it as a mono repo. we should use Nomad for the dev cluster instead of K8s. people can choose to run Nomad server along with solid trace console or separately based on what the cpu load comes out to be."

**What keeps the module separable inside one repo** (accepted with [What is the factory, and who is it for?](01-what-the-factory-is-and-who-its-for.md)):

- It imports nothing from `console/` or `event_store/`, and reaches SolidTrace only through public ingest, MCP and OAuth.
- It ships as its own image, off unless the operator turns it on, in its own top-level directory.
- No Nomad client runs on the SolidTrace machine, or agents would be placed there.
- Running the Nomad server beside Console joins them at the machine level: that server holds the agents' secrets and can run code on every client. It stays the operator's choice, and the docs should say so.

**Nomad's licence** (checked on `main`): Business Source License. A free, self-hosted factory is not a "competitive offering", so it's not a blocker. Don't ship Nomad inside our images.

**Still open:** how a run is isolated on a Nomad client ([How can a run be isolated on Nomad?](09-run-isolation-on-nomad.md)), and the record's load on the production instance.

Hard to reverse: an ADR when the spec is written.
