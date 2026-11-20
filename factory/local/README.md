# The local factory stack

A Docker Compose stack to build and test the factory on one machine, in place of the cloud resources. It has:

- **Nomad**: one server with ACLs, as in dev mode. It has no client of its own, so Nomad places each task on the agent machine.
- **One agent machine**: a container made from [agent-machine/Dockerfile](agent-machine/Dockerfile). It is a Nomad client with `raw_exec`. It has the run user `agent` (not root, in the `docker` group), the one sudoers rule of a take-over, `hc`, and Claude Code at the pinned version with its first-run dialogs accepted. It has no credentials.
- **The agent machine's firewall**: a container that owns the agent machine's network namespace, as a VPC firewall is outside a VM.
- **Squid**: the only way out, with the default allowlist of the reference configuration ([../reference/squid/allowlist.txt](../reference/squid/allowlist.txt)).

The dev SolidTrace runs on the host, as usual. Ticket 54 adds the Dispatcher and the fake Forge and Tracker to this stack. Ticket 46 makes the Dockerfile into the image recipe for agent VMs.

## Where it runs

The stack runs alone on the dev box (4 CPUs, 11 GB), beside the dev SolidTrace, with no flow stack running. Its containers have memory limits of about 2.2 GB in total. It needs Linux 5.14 or later with cgroup v2, and Docker with Compose v2. The first build downloads Claude Code (about 370 MB).

An agent runs each start, smoke check and image build through `~/.cache/solid-trace/software-factory-notes/slot.sh exclusive`, and stops the stack when it finishes.

## Start and stop it

From the repository root:

```sh
factory/local/stack up      # build the images, start the stack, wait for the agent machine
factory/local/stack down    # stop the stack, and delete its containers and volumes
factory/local/stack clean   # the same, and delete its images (about 1 GB)
factory/local/smoke         # start the stack, check it, and stop it again
```

The smoke check also runs the cgroup cases of the wrapper's kill boundary (`factory/internal/killboundary`) as root in a raw_exec task, with the processes of each case as the run user (`checks/kill-boundary-tests.nomad.hcl`). It builds their test binary with Go and no cgo, and copies it to the agent machine. With no Go, that check skips. Only this check: `SMOKE_ONLY='kill boundary' factory/local/smoke`.

It also runs the wrapper's run-user cases (`factory/wrapper`) as root in a raw_exec task, with the run user `agent` (`checks/run-user-tests.nomad.hcl`): the harness, its tools and git run as `agent` (git in the kill boundary), the wrapper makes its own directory, and `agent` cannot read the wrapper's secrets or change its files. Their test binary is static (the tests start a Dispatcher with SQLite through cgo), so the build needs Go and a C compiler. A last check (`checks/identity-file-check.nomad.hcl`) proves that `agent` cannot read a raw_exec task's identity file or reach the task's directory. Only these checks: `SMOKE_ONLY='run-user cases|identity file' factory/local/smoke`.

The images and Docker's build cache take about 1.5 GB of disk. When the disk is nearly full, use `stack clean` when you finish.

The compose project is `factory-local`. It has its own containers, networks and volumes, and publishes no port on the host. It does not touch the dev SolidTrace's containers or data, or a flow stack.

Other commands:

```sh
factory/local/stack nomad job status    # the Nomad CLI on the server, with the management token
factory/local/stack shell agent         # a shell on the agent machine as the run user
```

`FACTORY_STACK_PROJECT=<name>` gives the stack another compose project, so that an agent in a worktree does not use the stack of another agent.

## The Runner's tests

The Nomad Runner's tests (`factory/nomadrunner`, build tag `factorystack`) run against this stack's Nomad. They do not skip: with no stack, they fail. One command starts a stack with the compose project `factory-runner-test`, runs them, and stops the stack:

```sh
factory/local/test-runner                  # all of them
factory/local/test-runner -run Lost ./nomadrunner/
```

For these tests only, `FACTORY_STACK_NOMAD_API=1` adds [compose.nomad-api.yml](compose.nomad-api.yml): a forwarder that publishes Nomad's HTTP API on a free port of 127.0.0.1, because the host has no address on the internal factory network. Then `stack nomad-env` gives `NOMAD_ADDR`, `NOMAD_TOKEN` and `AGENT_MACHINE`. The test process is the Dispatcher. Each task prints what it got (such as its workload identity JWT) in place of the wrapper, and the test reads it through Nomad's API. The lost machine test pauses the agent machine's container (`docker pause`) until Nomad marks its node down, then continues it, and checks that the lost Execution's task stops and that Nomad does not place it again. These tests are also a step of the release suites ([docs/agents/testing.md](../../docs/agents/testing.md)).

The agent machine's Nomad client has `max_kill_timeout = "120s"`, as the Runner's job needs ([agent-machine/client.hcl](agent-machine/client.hcl)).

## The network rules

| From the agent machine | Result |
|---|---|
| Squid (`squid:3128`) | Allowed |
| Nomad RPC (`nomad:4647`) | Allowed |
| The Dispatcher's private listener (port `DISPATCHER_PORT`, default 7600, as `DISPATCHER_LISTEN`), direct: `NO_PROXY` has its host, `dispatcher` | Allowed |
| A host of the allowlist, through Squid (HTTPS on port 443, or HTTP on port 80) | Allowed |
| The dev EventStore, through Squid (`http://host.docker.internal:4000`) | Allowed |
| Each other host or port, through Squid | Squid refuses it (403) |
| Each other connection: the internet, the host, Nomad's HTTP API | Refused |
| A new connection into the agent machine | Refused |

The dev Console listens on 127.0.0.1 only, so the agent machine cannot reach it. Squid writes each request, with its host, to its log (`docker compose -p factory-local logs squid`).

Nomad's ACLs are on, so a process on the agent machine cannot use the local Nomad API with no token. `stack up` bootstraps the ACLs and keeps the management token in the server's volume.

ACLs do not stop a Nomad client from registering, so client introduction is strict: a client registers only with an introduction token. `stack up` makes one for the agent machine's client and writes it to the client's state directory, which only root can read. A Nomad client that the run user starts cannot register, so Nomad never places a Run's task on it. The smoke check proves this.

## A take-over on the local stack

The wrapper starts the `hc` core as the run user. Then a person attaches as a member of the access group, `factory-access`. On this stack that is the user `engineer`, which only the Dockerfile's `local` target has; the `agent-vm` target (the image recipe) has no member of the group:

```sh
docker exec -i -u engineer factory-local-agent-machine-1 sudo -u agent hc connect
```

Start the core before the first `hc connect`. When no core runs, `hc connect` starts one of its own, and that core gets the `SUDO_*` variables.

For a take-over (`solidtrace take-over <run id>`), the wrapper of the Run's Execution continues on the machine in a new Execution, in a new cgroup. It starts the core as `agent` in that cgroup, with no `SUDO_*` or `HC_*` variable, and starts `claude --resume <session id>` in a terminal named `take-over`. Then it checks that the core and its holder are in the cgroup. When they are not (a core that an early `hc connect` started, or one that systemd started), it kills them, and the take-over does not start. Wait until the Run page shows the take-over Execution as running before you attach. On this stack, set `DISPATCHER_TAKE_OVER_ATTACH='docker exec -i -u engineer factory-local-{vm} sudo -u {run_user} hc connect'`: `{vm}` is the Nomad node's name (`agent-machine-1`).

The smoke check runs the wrapper's take-over cases as root under raw_exec (`checks/take-over-tests.nomad.hcl`, cluster check 6). One case uses the image's real `hc`: the core is in the take-over Execution's cgroup with no `SUDO_` variable, and `engineer` reaches it with `sudo -u agent hc connect`. Only this check: `SMOKE_ONLY='take-over as root' factory/local/smoke`.

## What is different from an agent VM

- No Docker socket and no Docker daemon. The end procedure skips `docker kill`.
- No autoscaling, and no Machine notice from the cloud.
- No ssh: a take-over uses `docker exec`.
- The agent machine container has `SYS_ADMIN` and no AppArmor profile, so that cgroup v2 is writable and the Nomad client can mount each task's secrets directory. It has no `NET_ADMIN`, so no process on it can change the firewall.
- Root on the agent machine is root on the dev box, not root on a VM that holds one Run. The container shares the dev box's kernel with no user namespace, and `SYS_ADMIN` lets root remount `/proc/sys` and `/sys` writable. So the local stack never gives the run user a way to root: no Docker daemon in the container, no sudo for the run user, no setuid or setgid program other than Debian's own and `sudo`, and no program with a file capability. The smoke check proves this. If a later ticket needs Docker on the local stack, it uses a rootless daemon or a separate container, not a daemon in the agent machine container.

## Claude Code's first-run dialogs

The wrapper deletes the run user's home after each Execution, and it sets `CLAUDE_CONFIG_DIR`. So the image keeps the accepted dialogs in files that root owns, not in the home:

- `/etc/claude-code/managed-settings.json` (Claude Code's managed settings, which the run user cannot change): no bypass-mode warning, and no auto-updater, so that the pinned version stays.
- `/etc/factory/claude.json`: the template of the run user's `.claude.json`, with the onboarding done. For each Execution, the wrapper copies it to `$CLAUDE_CONFIG_DIR/.claude.json` and adds `projects["<checkout>"].hasTrustDialogAccepted`. Claude Code trusts a checkout only at its exact path.

The smoke check starts Claude Code in the same way: from a new, empty home and a new `CLAUDE_CONFIG_DIR`.

## The wrapper's fixed paths

The paths are the same on each agent machine, so that a resume works on any of them:

| Path | Default (environment variable) | After the Execution |
|---|---|---|
| The checkout of a repository: `<workspace>/<host>/<workspace>/<name>`, on the Run's branch `agent/<run id>` | `/workspace` (`WRAPPER_WORKSPACE`) | Deleted |
| The run user's home: `HOME`, `.gitconfig` (the credential helper, for the forge's git URL only, and git over ssh rewritten to HTTPS), and `CLAUDE_CONFIG_DIR` = `<home>/.claude` | `/home/agent` (`WRAPPER_HOME`) | Deleted |
| The caches: `XDG_CACHE_HOME` | `/var/cache/factory` (`WRAPPER_CACHE`) | Kept |

`CLAUDE_CODE_PROJECT_DIR_NAME` is the repository with each character that is not a letter or a digit changed to `-`, such as `bitbucket-org-team-shop`. In a monorepo, the agent's working directory is the Project's folder in the checkout. Before each end, the wrapper pushes each commit of the Run's branch that is not on the remote. This push reads no git configuration or hook that the agent can change. When it fails twice (for example, a person pushed to the branch), the commits go to `refs/agent-wip/<run id>`, and the Execution's end reason says so. The setup first deletes a workspace and a home that an earlier Execution left.
