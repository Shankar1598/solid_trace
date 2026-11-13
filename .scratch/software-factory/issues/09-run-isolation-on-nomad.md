# 09: How can a run be isolated on Nomad?

Type: research
Status: resolved
Map: [SolidTrace Software Factory](../map.md)

## Question

Agents run on Nomad clients, one allocation per run ([Where do agents run, and where does the factory's code live?](02-where-agents-run-and-where-the-code-lives.md)). A run executes code an agent wrote and holds repository write credentials. What can Nomad give it?

- **Task drivers:** Docker, Docker with gVisor (`runsc`), Podman, `exec`/`exec2`, and Firecracker or other microVM drivers. Which are maintained, which isolate a kernel, and what each costs in start time and setup.
- **Secrets:** Nomad Variables and workload identity. How a run gets a short-lived repository token and model key, and how they are kept out of the transcript.
- **Network egress:** can a run be limited to the forge, the model API and SolidTrace? Through Nomad's networking, CNI, or a proxy?
- **Parameterized batch jobs:** confirm the dispatch shape (`nomad job dispatch` with meta, a payload of at most 16 KiB) and how timeouts, retries and cancellation work; the event stream for run state.
- **Workspace:** cloning a repository per run, and caching it between runs on one client.

What Warp and the open-source orchestrators do for the same problem is in `~/.cache/solid-trace/software-factory-notes/warp.md` (local only) and [research/open-source-alternatives.md](../research/open-source-alternatives.md).

## Answer

Resolved 2026-11-13 (research). Full note: [research/nomad-run-isolation.md](../research/nomad-run-isolation.md).

- **Drivers.** Docker is the default. gVisor (`runtime = "runsc"`, forced with `allow_runtimes`) is the stronger option, works without nested virtualisation and is actively released. There is no maintained microVM driver: the Firecracker driver is stuck at Firecracker 0.25.2 and Nomad 1.9.7, and Virt is beta.
- **Secrets.** Nomad Variables can't hold per-run secrets, because a dispatched job's identity names its parent and every run of a job reads the same paths. Payload and meta are stored in the job. Use a broker: the task's workload identity JWT is checked against Nomad's JWKS, the run is found by allocation ID, and a one-hour GitHub installation token for one repository comes back.
- **Structural split.** A prestart `checkout` task, a main `agent` task with no forge credential, and a poststop `publish` task that pushes from a `git bundle`. The model key stays in a gateway. Neither secret can then reach the transcript.
- **Egress.** Bridge or custom CNI, plus admin-chain iptables rules that drop the metadata server and private ranges. A domain allowlist needs a forward proxy.
- **Dispatch.** 16 KiB payload, meta, the run ID as idempotency token. `max_run_duration` (Nomad 2.0+) marks a timeout `complete`, not `failed`. Restart and reschedule set to 0. Cancel with `job stop`. The event stream needs reconciliation after a reconnect.
- **Workspace.** Clone per run into the alloc dir, from a read-only per-client mirror with `--reference-if-able --dissociate`. Keep Docker images longer than the 3-minute GC default.
