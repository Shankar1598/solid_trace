# The reference configuration

The files that an operator copies to the server VM and the agent VMs. [The operator guide](../../docs/factory.md) says where each file goes and in what order. Your own infrastructure code makes the VMs: SolidTrace gives no Terraform.

| File | For | What it does |
|---|---|---|
| [docker-compose.yml](docker-compose.yml) | The server VM | Runs the Dispatcher's image on the host's network, with its database in a volume. |
| [dispatcher.env.example](dispatcher.env.example) | The server VM | Each setting of the Dispatcher, with its meaning. |
| [nomad/server.hcl](nomad/server.hcl) | The server VM | The Nomad server: ACLs on, and a client registers only with an introduction token. |
| [nomad/dispatcher-policy.hcl](nomad/dispatcher-policy.hcl) | The server VM | The Nomad ACL policy of the Dispatcher's token. |
| [nomad/client.hcl](nomad/client.hcl) | Each agent VM | The Nomad client: the server's address and the node pool of the machine kind. |
| [nomad/autoscaler-policy.hcl](nomad/autoscaler-policy.hcl) | The server VM | The Nomad ACL policy of the Autoscaler's token. |
| [autoscaler/](autoscaler) | The server VM | The Nomad Autoscaler: its agent file, one scaling policy for each machine kind, the fallback policy (Prometheus), and a Compose file. The guide's [Scaling](../../docs/factory.md#scaling) section says how to use them. |
| [squid/allowlist.txt](squid/allowlist.txt) | The server VM | The default allowlist: the hosts that Claude Code, git, and the usual package tools need. |
| [squid/squid.conf](squid/squid.conf) | The server VM | Squid, the only way out for the agent VMs. |
| [systemd/](systemd) | The server VM | Reloads Squid when the Dispatcher changes the allowlist. |
| [caddy/Caddyfile](caddy/Caddyfile) | The server VM | The public route to the Dispatcher's webhook listener. |
| [factory.toml](factory.toml) | A repository | An example of the factory file. |

The firewall rules are in [the operator guide](../../docs/factory.md#the-network), with an example for Google Cloud.

## The Nomad job template

The Dispatcher fills this template. You do not write it or change it. These notes are for an operator who reads the jobs in Nomad.

- **One job for each machine kind.** The Dispatcher registers the parameterized batch job `factory-execution-<kind>` when it first starts an Execution of that kind. A new start of the Dispatcher registers the jobs again, so a change applies after a restart.
- **One dispatch for each Execution.** The dispatch has two meta values, `run` (the Run's id) and `execution` (the Execution's id), and no payload. The meta values hold no secret.
- **The node pool is the kind's name.** With no kind in the factory file, the job is in the operator's default kind. The job takes all the memory that the kind names (`DISPATCHER_MACHINE_KINDS`), so one Run works on one VM.
- **`raw_exec`**, on clients that run only agents. The command is `/usr/local/bin/factory-wrapper`, which the image recipe installs.
- **No restart and no reschedule.** A failed Execution makes the Run "failed", and a message continues it. A lost machine is the one case that the Dispatcher restores, in a new Execution.
- **`kill_timeout` is 120 seconds**, with the signal `SIGTERM`, and the client's `max_kill_timeout` is 120 seconds (the image recipe sets it). The wrapper uses this time to save the Run's work.
- **The workload identity** has the name `dispatcher` and the audience `solidtrace-dispatcher`. Nomad puts it in a file in the task's secrets directory, and the wrapper sends it to the Dispatcher. The Dispatcher checks it against Nomad's key set (`/.well-known/jwks.json`) and against the allocation that Nomad gives it. The job has no Vault and no Consul.
- **The environment of the task** has `WRAPPER_DISPATCHER_URL`, `WRAPPER_EXECUTION_ID`, `WRAPPER_IDENTITY_FILE` and `WRAPPER_DIR`. It has no token and no secret. The wrapper gets the secrets of an Execution from the Dispatcher, after it shows its identity.
- **`max_run_duration` is not used.** The limits of an Execution are in the factory file. When an Execution passes its total limit plus 15 minutes, the Dispatcher stops its job.
