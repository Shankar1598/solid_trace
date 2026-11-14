# Agents run on Nomad, never on the SolidTrace instance; the factory is a separable module

The factory gives work to coding agents. Each **Execution** of a **Run** works on an agent VM of its own, which a Nomad cluster schedules. No agent runs on the machine that runs Console and EventStore. The factory's code is in this repository, as a module of its own: the **Dispatcher** (one Go binary with SQLite, beside the Nomad server) and the wrapper that each Execution runs. It imports nothing from `console/` or `event_store/`, ships as its own image, is off unless the operator turns it on, and reaches SolidTrace only through public ingest, the MCP server and OAuth.

We chose this because the SolidTrace instance is busy with ingest and Log processing, and an agent runs code that it wrote, with a write token for the team's repositories. One rule divides the work: SolidTrace stores and shows; the factory acts.

## Considered options

- **Agents on the SolidTrace instance:** no new machine, but an agent's tests and builds take the CPU that ingest needs, and code that an agent wrote would run beside the stored data.
- **Kubernetes:** more teams know it, but it is much more to run for a team of about ten engineers. Nomad runs a whole VM as one task with `raw_exec`, which is what one Run on each VM needs.
- **The factory in a repository of its own:** a cleaner boundary, but two release lines for one product. The import rule above keeps the boundary inside one repository.
- **Factory settings in Console's database:** Console would get tables that only the factory uses, and the Dispatcher needs them for each webhook and poll, also while SolidTrace is down. So the Dispatcher's database holds them, keyed by the SolidTrace **Project** id, and Console edits them through the Dispatcher's private API as the signed-in user.

## Consequences

- SolidTrace still runs on one machine with no cluster. A team that does not use the factory runs nothing new.
- Console stores nothing of the factory. It reads Runs from **Logs** that the Dispatcher sends through public ingest. It calls the Dispatcher only for the Stop button, the Factory section and the Factory **Integration**, at a URL that an **Instance admin** sets, with a short-lived token for the signed-in user.
- Nomad's licence is the Business Source License. A free, self-hosted factory is not a competing offer, but no SolidTrace image ships Nomad; the operator installs it. The factory uses the `nomad/api` Go module, which is MPL-2.0.
- No Nomad client runs on the SolidTrace machine, or Nomad could place agents there. A Nomad server beside Console is the operator's choice, and it joins them at the machine level: that server holds the agents' secrets and can run code on each client.
- The parts that are one team's setup (Bitbucket Cloud, Claude Code, subscription tokens, Notion and Linear, ssh and `hc` for a take-over, GCE's preemption notice, one Run on each VM) sit behind seven interfaces: Forge, Runner, Harness, Credential source, Tracker, Take-over and Machine notice.

Source: [.scratch/software-factory-mvp/spec.md](../../.scratch/software-factory-mvp/spec.md)
