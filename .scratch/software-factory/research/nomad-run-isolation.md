# How a run can be isolated on Nomad (research note)

Answers [09: How can a run be isolated on Nomad?](../issues/09-run-isolation-on-nomad.md). Checked on 2026-11-13 against HashiCorp's docs, Nomad's source on `main`, driver repos, gVisor, Firecracker, GitHub, Google Cloud, Claude Code and Codex docs. Nothing was installed or run. Claims not confirmed from a primary source are marked **(unverified)**.

Current Nomad: 2.1.1 Community Edition, 2026-10-29 ([CHANGELOG](https://github.com/hashicorp/nomad/blob/main/CHANGELOG.md)).

## Summary

- **Drivers.** Docker is built in and the only practical container driver. gVisor plugs into it as `runtime = "runsc"`, needs no nested virtualisation, and ships weekly. Podman and exec2 are HashiCorp plugins; exec2 shares the host kernel. There is **no maintained microVM driver**: the community Firecracker driver stops at Firecracker 0.25.2 and Nomad 1.9.7, and the Virt driver is beta, "Do not use this driver in production".
- **Secrets.** Nomad Variables don't fit per-run secrets. A dispatched job's workload identity names the **parent** job, so every run of a job can read the same variables. The dispatch payload and meta are kept in the job's state, so secrets can't go there either. The clean path is the workload identity JWT, checked by a factory-side broker against Nomad's public JWKS. The broker then returns a one-hour GitHub installation token for one repository.
- **Structural fix.** Keep the forge write token out of the agent's task: a prestart task clones and a poststop task pushes. Keep the model key out of the run: a gateway holds it. Then the transcript can't leak either one.
- **Egress.** Nomad has no allowlist by domain. Bridge or custom CNI networking, plus the operator's iptables rules in the admin chain, can block the cloud metadata server and the private network. An allowlist needs a forward proxy. Consul's transparent proxy needs a Consul service mesh.
- **Dispatch.** Confirmed: a payload of at most 16 KiB, `meta_required` and `meta_optional`, an idempotency token and an ID prefix. Nomad 2.0 added the group setting `max_run_duration`, which marks a timed-out run `complete`, not `failed`. Batch defaults retry a run 3 times in place and reschedule it once, so turn both off. Cancel with `nomad job stop`. Run state comes from `/v1/event/stream`, which is resumable only while the index is still in its buffer.
- **Workspace.** Clone into the allocation directory. Cache a bare mirror per client on a read-only host volume, and clone with `--reference-if-able --dissociate`. Raise Docker's image GC delay (default 3 minutes) or each sparse run pulls the image again.

## Recommendation

**First release (default).**

- The Docker driver, on a dedicated node pool of agent-only clients. Never on the SolidTrace machine, as [02](../issues/02-where-agents-run-and-where-the-code-lives.md) already says.
- A non-root user, `privileged` off (the default), `cap_drop = ["all"]` with only what's needed added back, and a `pids_limit`.
- One allocation and three tasks:
  - prestart `checkout`: gets a read-only token from the broker and clones;
  - main `agent`: runs the harness, with no forge credential;
  - poststop `publish`: takes a `git bundle` from the agent and pushes it to a branch the dispatcher named, then opens the PR.
- The model key lives in a gateway. The agent gets only a per-run credential for the gateway.
- `bridge` networking, with admin-chain rules that drop traffic to `169.254.169.254` and to private ranges. The public internet stays open, because package registries are needed.
- `max_run_duration` set, `restart` and `reschedule` attempts set to 0. The dispatcher decides any retry.

**Stronger option.**

- Add `runtime = "runsc"` (gVisor), and set `allow_runtimes = ["runsc"]` on the agent clients so a job can't choose `runc`. Cost: some syscalls are missing and syscall-heavy builds run slower. It's the next step when triggers carry untrusted text (public issues, outside PRs) or several teams share clients.
- Add an egress allowlist through a forward proxy: the forge, the model gateway, SolidTrace and named registries.
- A microVM per run is out of reach on Nomad today. The nearest equivalent is a Nomad client VM per team or trust level.

## 1. Task drivers

The docs say that drivers "don't serve as a security boundary for the client-to-task interface" and that isolation "depends upon individual task driver implementations" ([task drivers](https://developer.hashicorp.com/nomad/docs/drivers)).

| Driver | Shipped / maintained | Kernel isolation | Setup | Start time |
|---|---|---|---|---|
| Docker (`runc`) | Built in | None: shares the host kernel (namespaces, cgroups, seccomp) | Docker on each client | Under a second with a warm image **(unverified, no primary figure)**. The image pull dominates. |
| Docker + gVisor (`runsc`) | Built-in driver. gVisor releases weekly (release-20261109.0 on 2026-11-11, [releases](https://github.com/google/gvisor/releases)) | Yes: a user-space "application kernel" in Go; the Sentry "does not pass system calls through to the host kernel" ([gVisor docs](https://gvisor.dev/docs/)) | `runsc install`, restart Docker ([quick start](https://gvisor.dev/docs/user_guide/quick_start/docker/)), add `runsc` to `allow_runtimes` (default `["runc", "nvidia"]`, [Docker driver](https://developer.hashicorp.com/nomad/docs/drivers/docker)) | "fast startup", no figure given ([gVisor docs](https://gvisor.dev/docs/)) |
| Podman | Plugin. "HashiCorp maintains the plugin"; 0.6.4–0.6.6 support Nomad 1.8–2.0 ([Podman](https://developer.hashicorp.com/nomad/plugins/drivers/podman)) | None. It can run rootless, and has `userns`, `cap_drop` and `security_opt` | `podman` plus the plugin binary; rootless needs cgroups v2 | As Docker **(unverified)** |
| exec | Built in | None: chroot, cgroups, private PID and IPC namespaces ([exec](https://developer.hashicorp.com/nomad/docs/drivers/exec)) | Nomad as root | Fast, no image |
| exec2 | Plugin, in HashiCorp's GitHub org; 91 commits, no releases listed ([repo](https://github.com/hashicorp/nomad-driver-exec2)) | None: Landlock, cgroups v2, namespaces, dynamic workload users ([exec2](https://developer.hashicorp.com/nomad/plugins/drivers/exec2)) | Linux 5.15+, Landlock enabled (not on RHEL), Nomad as root | Fast, no image |
| Firecracker (community) | Hobby: "I work on this project on my free time"; "The last version of firecracker that works with this nomad plugin is : 0.25.2"; supports up to Nomad 1.9.7 ([repo](https://github.com/cneira/firecracker-task-driver)) | Yes, a KVM microVM | KVM, CNI with tc-redirect-tap, a kernel and rootfs per image | The Firecracker VMM is up in "8 CPU ms" ([spec](https://github.com/firecracker-microvm/firecracker/blob/main/SPECIFICATION.md)); guest boot is extra |
| Virt (libvirt/QEMU) | Plugin, beta: "Do not use this driver in production environments" ([Virt](https://developer.hashicorp.com/nomad/plugins/drivers/virt)) | Yes, a full VM | libvirt; network isolation "host" only | Not stated |
| containerd (community) | "actively looking for contributors and maintainers"; v0.9.3 ([repo](https://github.com/Roblox/nomad-driver-containerd)) | Depends on `containerd_runtime`; runsc and Kata support **unconfirmed** | containerd | Not stated |

Notes:

- No Kata, Cloud Hypervisor or gVisor-specific driver is on HashiCorp's [community list](https://developer.hashicorp.com/nomad/plugins/drivers/community).
- On Google Cloud, gVisor's default `systrap` platform suits a VM without nested virtualisation ([platforms](https://gvisor.dev/docs/architecture_guide/platforms/)).
- Firecracker or KVM on Compute Engine needs nested virtualisation. E2, Arm and most AMD machine types don't support it, and CPU-bound work runs "10% or greater" slower ([GCP](https://docs.cloud.google.com/compute/docs/instances/nested-virtualization/overview)).
- The Docker task options that matter are `runtime`, `cap_drop`, `privileged`, `readonly_rootfs`, `pids_limit`, `security_opt` and `network_mode` ([Docker task config](https://developer.hashicorp.com/nomad/docs/job-declare/task-driver/docker)).
- The harnesses have their own sandboxes inside the container. One example is Codex's `sandbox_mode` and its `features.network_proxy` domain rules, which are "off by default" ([Codex config](https://learn.chatgpt.com/docs/config-file/config-reference)). They are a second layer, not the boundary.

## 2. Secrets

**What Nomad gives.**

- **Workload identity.** Every task gets a JWT signed by the leader's keyring. It is "not exposed to tasks by default"; `env = true` gives `NOMAD_TOKEN` and `file = true` gives `secrets/nomad_token` ([concept](https://developer.hashicorp.com/nomad/docs/concepts/workload-identity)).
  - Extra `identity` blocks take `aud`, `ttl` (renewed at about half), `change_mode` and `extra_claims` ([identity block](https://developer.hashicorp.com/nomad/docs/job-specification/identity)).
  - The claims are `nomad_namespace`, `nomad_job_id`, `nomad_allocation_id` and `nomad_task`. Job meta is a claim only through namespace-defined `extra_claims`.
  - Third parties verify the JWT through `/.well-known/jwks.json`, which needs no ACL. `/.well-known/openid-configuration` also needs no ACL but is off until `oidc_issuer` is set ([keyring API](https://developer.hashicorp.com/nomad/api-docs/operator/keyring)).
- **Nomad Variables.** Encrypted and replicated by Raft, but "Nomad stores the root encryption key on the servers". Meant for "small pieces of configuration data" ([variables](https://developer.hashicorp.com/nomad/docs/concepts/variables)). Tasks read them with `nomadVar` in a `template` ([template](https://developer.hashicorp.com/nomad/docs/job-specification/template)).
- **Task API.** A Unix socket at `${NOMAD_SECRETS_DIR}/api.sock` that "always requires authentication". If `task.user` is set, only that user can use it ([Task API](https://developer.hashicorp.com/nomad/api-docs/task-api)).

**Findings that shape the design.**

- **Every run of a job shares variable access.** The ACL code grants read and list on exact paths only: `nomad/jobs`, `nomad/jobs/<job>`, `.../<group>` and `.../<group>/<task>` ([acl.go](https://github.com/hashicorp/nomad/blob/main/acl/acl.go), `matchingVariablesCapabilitySet`). The identity's job is `GetIDforWorkloadIdentity()`, which returns `ParentID` for a dispatched job ([structs.go](https://github.com/hashicorp/nomad/blob/main/nomad/structs/structs.go)). So all concurrent runs of one parameterized job read the same variables. A per-run token stored there is visible to every other run of that job.
- **Payload and meta are stored in the job.** The `Job` struct holds "the payload supplied when the job was dispatched" (structs.go). Anyone with read-job on the namespace can see it. Never put secrets there.
- **Alloc logs.** Nomad captures stdout and stderr ([task `logs`](https://developer.hashicorp.com/nomad/docs/job-specification/task)). If the harness streams its transcript to stdout, anyone with log access reads it.

**Forge token.**

- A GitHub App installation token expires after one hour. It can be cut down to named `repositories` and `permissions`, but no further than the installation's own grant ([GitHub](https://docs.github.com/en/rest/apps/apps#create-an-installation-access-token-for-an-app)).
- GitHub doesn't take Nomad's JWT directly, so a factory-side **broker** has to mint the token. The flow (design, not tested):
  1. The task presents its JWT, with `aud = "solidtrace-factory"` and a short `ttl`.
  2. The broker checks it against the JWKS.
  3. The broker maps `nomad_allocation_id` to a run through Nomad's API. Per-run scope can't come from `nomad_job_id`, which names the parent.
  4. The broker checks `nomad_task`: only `checkout` gets read and only `publish` gets write.
  5. It mints a token for that one repository.
- Each task's `secrets/` directory is its own, so the agent task can't read the `publish` task's JWT **(unverified for Docker; follows from per-task secrets dirs)**.
- Untrusted git data: the `publish` task should take a `git bundle` the agent wrote, fetch it into a fresh clone, and push. It should never run git inside the agent's working tree, where hooks or config could execute code.
- `max_run_duration` doesn't start poststop tasks ([group](https://developer.hashicorp.com/nomad/docs/job-specification/group)), so a timed-out run publishes nothing.
- Whether poststop runs after `nomad job stop` is **unverified**.

**Model key.**

- Keep it out of the run. Claude Code supports a gateway through `ANTHROPIC_BASE_URL` with a gateway credential, and "the provider key stays server-side" ([LLM gateway](https://code.claude.com/docs/en/llm-gateway)). It also supports `apiKeyHelper`, which it reloads on change ([settings](https://code.claude.com/docs/en/settings)).
- Codex takes `model_providers.<id>.base_url` and an `env_key` in the user-level `~/.codex/config.toml` ([Codex config](https://learn.chatgpt.com/docs/config-file/config-reference)).
- The run's gateway credential can be its own workload identity JWT, checked by the gateway against the JWKS (design, **unverified with each harness**). Or the broker can mint an opaque per-run key that it revokes at run end. Either one dies with the run, and the gateway enforces a budget.

**Out of the transcript.** With both secrets structurally absent from the agent's task, the transcript can hold only the gateway credential, which expires, plus any secrets inside the repository itself. Server-side redaction stays with [13](../issues/13-transcript-redaction-and-retention.md).

## 3. Network egress

- **Modes:** `none`, `bridge`, `host` and `cni/<name>` ([network](https://developer.hashicorp.com/nomad/docs/job-specification/network)).
  - `bridge` uses the reference CNI plugins (loopback, bridge, firewall, portmap) on a default subnet of `172.26.64.0/20`.
  - The firewall plugin "does not add, delete, or modify rules in the admin chain". The operator's rules there run first ([CNI](https://developer.hashicorp.com/nomad/docs/networking/cni)).
  - A custom `cni/agent` network gives agent runs their own subnet and rules, apart from other jobs on the client.
- **The metadata server must be blocked.** On Google Cloud, "any process that can query the metadata URL has access to all values in the metadata server", and Google advises to "sandbox any process that shouldn't be able to access" it ([GCP metadata](https://docs.cloud.google.com/compute/docs/metadata/overview)). That reaches the VM's service account. Drop `169.254.169.254`, and the VPC ranges (Nomad, Consul and internal services), in the admin chain.
- **Nomad has no allowlist by domain.** For "only the forge, the model API and SolidTrace", run a forward proxy that allows CONNECT to named hosts, and let the firewall pass only the proxy (and DNS). Squid, Envoy and similar would work; none was evaluated here.
- **Consul `transparent_proxy`** adds `consul-cni` to the chain ([CNI](https://developer.hashicorp.com/nomad/docs/networking/cni)). It needs a Consul service mesh, which is too heavy for this.
- gVisor uses its own network stack, but the host-side bridge rules still apply **(unverified detail)**.

## 4. Dispatch shape

**The parameterized job** ([parameterized](https://developer.hashicorp.com/nomad/docs/job-specification/parameterized), [dispatch](https://developer.hashicorp.com/nomad/docs/commands/job/dispatch)):

- `payload` is `optional`, `required` or `forbidden`. Its maximum is "16384 bytes (16KiB)", written to a file by `dispatch_payload`.
- `meta_required` and `meta_optional` are read as `${NOMAD_META_<key>}`.
- Each dispatch gets a new job ID, `<parent>/dispatch-<ts>-<suffix>`. `-id-prefix-template` puts a prefix after `dispatch-`.
- A dispatched job can't be updated.
- Pass `run_id` in meta and fetch the prompt by reference.

**Idempotency.** `-idempotency-token` blocks a dispatch only while a sibling with the same token is still non-terminal (`DispatchIdempotencyToken` in structs.go). Use the run ID.

**Timeout.** Set `max_run_duration` on the group, batch and sysbatch only. It appears in the v2.0.x docs and not in v1.11.x.

- The timer starts when the client creates the allocation, so image and artifact downloads count.
- Restarts don't reset it.
- On timeout the allocation is `complete` with "allocation exceeded max_run_duration". It is not `failed`, so reschedule doesn't fire, and poststop tasks don't start ([group](https://developer.hashicorp.com/nomad/docs/job-specification/group)).
- The dispatcher must read that description to record a timeout.

**Retries.** The batch defaults are restart 3 attempts in 24h with a 15 s delay and `fail` mode, and reschedule 1 attempt in 24h (`DefaultBatchJobRestartPolicy` and `DefaultBatchJobReschedulePolicy` in structs.go). An agent run isn't idempotent, so set both to 0 and let the dispatcher retry with a new dispatch.

**Cancel.** `nomad job stop <dispatched id>`, with `-purge` to remove it ([job stop](https://developer.hashicorp.com/nomad/commands/job/stop)).

- Docker tasks get SIGTERM, then a kill after `kill_timeout` (default 5 s). That is capped by the client's `max_kill_timeout`, default 30 s ([task](https://developer.hashicorp.com/nomad/docs/job-specification/task)).
- Raise both if the harness has to upload a transcript when it stops.

**Run state** ([events](https://developer.hashicorp.com/nomad/api-docs/events)):

- `GET /v1/event/stream`, NDJSON with heartbeats. Topics include `Job` and `Allocation` (`AllocationCreated`, `AllocationUpdated`, `AllocationUpdateDesiredStatus`).
- Filter with `topic=Allocation:<key>`, and resume with `index`.
- "If the requested index is no longer in the buffer the stream will start at the next available index". So the dispatcher must reconcile by listing jobs and allocations after a reconnect, as Symphony's SPEC does.
- The buffer size isn't stated on that page **(unverified)**.

**Queueing.** Nomad 2.1.0 added an experimental batch job queue on node pools, with FIFO or Dynamic Priority ([CHANGELOG](https://github.com/hashicorp/nomad/blob/main/CHANGELOG.md)). It could hold runs while the cluster is full.

## 5. Workspace and caching

- **Per run:** the `checkout` prestart task clones into `${NOMAD_ALLOC_DIR}`, which all tasks in the allocation share. Main tasks wait for prestart tasks ([lifecycle](https://developer.hashicorp.com/nomad/docs/job-specification/lifecycle)).
- **Between runs on one client:**
  - Keep a bare mirror per repository on a host volume. Mount it `read_only` ([volume](https://developer.hashicorp.com/nomad/docs/job-specification/volume)) into `checkout` only.
  - Clone with `--reference-if-able <mirror> --dissociate`. The new repository copies the objects it borrowed and stops depending on the mirror ([git clone](https://git-scm.com/docs/git-clone)).
  - Avoid `--shared`, which the git docs call "possibly dangerous".
  - A separate per-client periodic job is the mirror's only writer, so a run can't poison it.
  - Dynamic host volumes, with the built-in `mkdir` plugin, can create cache volumes through the API ([host volumes](https://developer.hashicorp.com/nomad/docs/other-specifications/volume/host)).
- **`ephemeral_disk` `sticky` and `migrate`** carry data to a *replacement* allocation ([ephemeral_disk](https://developer.hashicorp.com/nomad/docs/job-specification/ephemeral_disk)). Each dispatch is a new job, so they don't help **(not stated for dispatched jobs)**.
- **Image cache:** the Docker driver's `gc.image` defaults to true with `image_delay = 3m` ([Docker driver](https://developer.hashicorp.com/nomad/docs/drivers/docker)). Agent clients should keep images longer. `image_pull_timeout` is 5m by default, and the pull counts against `max_run_duration`.
- Dependency caches (npm, Go modules) can follow the same read-only host-volume pattern. Not researched further.

## 6. What Warp and the open-source projects do

From `~/.cache/solid-trace/software-factory-notes/warp.md` (local only), not re-researched:

- Warp-hosted runs get a per-run Docker sandbox that is destroyed afterwards. Containers run as a non-root `agent` user with passwordless sudo.
- Self-hosted workers use Docker, a Kubernetes Job, Direct or Command backends, and isolation "depends on your cluster configuration".
- Secrets are scoped at sandbox provisioning.
- Egress is limited by `networkPolicy` in the factory definition, and the Kubernetes support for it is "pending".
- There are pre-warmed sandboxes and optional snapshots for handoff.
- Agents sharing a host should use separate worktrees.

From [open-source-alternatives.md](open-source-alternatives.md): E2B uses Firecracker microVMs (KVM, single-host when self-hosted), and kubernetes-sigs/agent-sandbox uses gVisor or Kata through a `RuntimeClass`. Neither runs on Nomad.

## Open questions this raises

- **Mid-run push.** Must a run push or read CI results while it runs (the "hands and eyes" loop)? If so, the agent task needs a write token after all, and the forge's branch rules carry the restriction.
- **Docker inside a run.** Do runs need Docker for tests (testcontainers, compose)? That pushes towards privileged containers or Docker inside gVisor, which wasn't checked.
- **Broker and gateway placement.** Do the broker and the model gateway belong in the dispatcher service ([10](../issues/10-dispatcher.md)), or apart from it?
- **Poststop on cancel.** Does a poststop task run after `nomad job stop`? This needs a quick test on a real cluster.
