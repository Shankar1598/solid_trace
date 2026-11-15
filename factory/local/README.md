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

The images and Docker's build cache take about 1.5 GB of disk. When the disk is nearly full, use `stack clean` when you finish.

The compose project is `factory-local`. It has its own containers, networks and volumes, and publishes no port on the host. It does not touch the dev SolidTrace's containers or data, or a flow stack.

Other commands:

```sh
factory/local/stack nomad job status    # the Nomad CLI on the server, with the management token
factory/local/stack shell agent         # a shell on the agent machine as the run user
```

## The network rules

| From the agent machine | Result |
|---|---|
| Squid (`squid:3128`) | Allowed |
| Nomad RPC (`nomad:4647`) | Allowed |
| The Dispatcher's private listener (port `DISPATCHER_PORT`, default 4700) | Allowed |
| A host of the allowlist, through Squid (HTTPS on port 443, or HTTP on port 80) | Allowed |
| The dev EventStore, through Squid (`http://host.docker.internal:4000`) | Allowed |
| Each other host or port, through Squid | Squid refuses it (403) |
| Each other connection: the internet, the host, Nomad's HTTP API | Refused |
| A new connection into the agent machine | Refused |

The dev Console listens on 127.0.0.1 only, so the agent machine cannot reach it. Squid writes each request, with its host, to its log (`docker compose -p factory-local logs squid`).

Nomad's ACLs are on, so a process on the agent machine cannot use the local Nomad API with no token. `stack up` bootstraps the ACLs and keeps the management token in the server's volume.

## A take-over on the local stack

The wrapper starts the `hc` core as the run user. Then a person attaches as a member of the access group, `factory-access` (the user `engineer` on this stack):

```sh
docker exec -i -u engineer factory-local-agent-machine-1 sudo -u agent hc connect
```

Start the core before the first `hc connect`. When no core runs, `hc connect` starts one of its own, and that core gets the `SUDO_*` variables.

## What is different from an agent VM

- No Docker socket and no Docker daemon. The end procedure skips `docker kill`.
- No autoscaling, and no Machine notice from the cloud.
- No ssh: a take-over uses `docker exec`.
- The agent machine container has `SYS_ADMIN` and no AppArmor profile, so that cgroup v2 is writable and the Nomad client can mount each task's secrets directory. Root on it is much as root on a VM. It has no `NET_ADMIN`, so no process on it can change the firewall.
- Claude Code trusts a checkout only at its exact path. The wrapper adds `projects["<checkout>"].hasTrustDialogAccepted` to the run user's `.claude.json` before it starts Claude Code.
