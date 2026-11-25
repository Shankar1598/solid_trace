# The Factory: operator guide

The **Factory** gives work to coding agents on your own machines. A person, a tracker ticket, a schedule or a webhook starts a **Run**. The **Dispatcher** queues the Run and starts its **Executions**. Each Execution works on its own agent VM. SolidTrace keeps the record of each Run.

This guide goes from an empty server VM to a first Run. Read [Agent sessions](agent-sessions.md) for what people do with Runs.

On Google Cloud, the wizard `factory/setup/gcp-wizard` does these steps with you, from an empty project to a first Run that opens a PR on a Bitbucket test repository. It asks where the server runs (on the SolidTrace VM, or on a VM of its own), makes one agent VM and adds one Claude account. For each command, you choose: the wizard runs it, you run it yourself, or you skip it. It keeps its values in `~/.config/solidtrace-factory/gcp/values.env`, and it does not set up webhooks, autoscaling or take-over.

## What you install

| Part | Where | What it is |
|---|---|---|
| The Dispatcher | The server VM | One container, from the release's image `ghcr.io/shankar1598/solid-trace-dispatcher`, for amd64 and arm64. It keeps its state in one SQLite file. |
| Nomad | The server VM and the agent VMs | One server and one client for each agent VM. Nomad is not in any SolidTrace image (its licence is BSL). You install it from HashiCorp's packages. |
| The Nomad Autoscaler | The server VM | Optional. It adds and removes agent VMs ([Scaling](#scaling)). It is HashiCorp's image, not SolidTrace's. |
| Squid | The server VM | The only way out for the agent VMs. It allows only the hosts of an allowlist. |
| Caddy | The server VM | The public route to the Dispatcher's webhook listener. You can use your own load balancer in place of it. |
| The agent VM image | Each agent VM | You bake it with [the recipe](../factory/image/README.md). It has Claude Code, the wrapper, the agent CLI and the run user. |

The release has these files, beside the images:

| File | Use |
|---|---|
| `factory-bin-linux-amd64.tar.gz` | The wrapper (`factory-wrapper`) and the agent CLI (`solidtrace`), static, for the image recipe. |
| `factory-image-recipe.tar.gz` | The image recipe (`agent-vm.sh` and its `files/`). |
| `factory-SHA256SUMS` | The SHA-256 of the two archives. Check them with `sha256sum -c`. |

The reference configuration is in [`factory/reference/`](../factory/reference/README.md). Each file in this guide is there.

## Rules

- **No agent runs on the SolidTrace VM.** Agents use much CPU and memory. They must not slow the ingest of Logs. Never run a Nomad client there. The Dispatcher, the Nomad server and Squid go on a server VM of their own. For a pilot or a small team, they can go on the SolidTrace VM, beside Console ([ADR 0019](adr/0019-agents-run-on-nomad-and-the-factory-is-a-separable-module.md)). They use about 1 GB of memory there. That VM then holds the agents' secrets, and its Nomad server can run code on each agent VM.
- **The factory is off unless you turn it on.** The files of a SolidTrace install (`deploy/`) have no part of the factory. A SolidTrace install with no Dispatcher URL (Settings → Instance) shows no part of the factory in Console: no Factory section, no Stop button and no Factory Integration provider. The Dispatcher itself refuses to start with no configuration.
- **The Dispatcher's API is never public.** It listens only on the private address that you give. Only the webhook listener is public, and it serves only `POST /webhooks/<kind>/<id>`.
- **One Run works on one VM.** The job of an Execution takes all the memory of the VM.

## The network

Agent VMs run code that an agent wrote. They can reach only the server VM.

| From | To | Port | Why |
|---|---|---|---|
| An agent VM | The server VM | 3128 | Squid. All internet traffic, and SolidTrace's ingest, goes through it. |
| An agent VM | The server VM | 4647 | Nomad's RPC. |
| An agent VM | The server VM | 7600 (the port of `DISPATCHER_LISTEN`) | The Dispatcher's private listener. |
| Google's IAP range (`35.235.240.0/20`) | An agent VM | 22 | ssh, for a take-over and for you. This is the only inbound connection. |
| The internet | The server VM | 443 | Caddy, for the webhooks. |
| The SolidTrace VM, and each engineer's machine | The server VM | 7600 | The Dispatcher's API: Console's calls and the `solidtrace` verbs. Use a private network or a VPN. |

Each other connection from an agent VM is refused. In particular, an agent VM does not reach Nomad's HTTP API (port 4646), SolidTrace's Console, production or staging.

- Each agent VM has a service account of its own, with **no roles** and no access scopes.
- The metadata and the startup script of an agent VM hold **no secrets**. Put the introduction token of the Nomad client in the VM's data directory, not in its metadata.
- The metadata server stays reachable. DNS goes through it.
- git over ssh is rewritten to HTTPS on the agent VM. The tools use `HTTPS_PROXY`, and the Docker daemon has its own proxy setting. The image recipe sets them.

An example for Google Cloud. Give the tag `factory-agent` to each agent VM and `factory-server` to the server VM. Change the network name and the server VM's address (`10.100.0.3`):

```sh
# The agent VMs: the only way out is the server VM, on three ports.
gcloud compute firewall-rules create factory-agent-egress-allow --network=factory --direction=EGRESS \
  --priority=1000 --action=ALLOW --rules=tcp:3128,tcp:4647,tcp:7600 \
  --destination-ranges=10.100.0.3/32 --target-tags=factory-agent
gcloud compute firewall-rules create factory-agent-egress-deny --network=factory --direction=EGRESS \
  --priority=2000 --action=DENY --rules=all --destination-ranges=0.0.0.0/0 --target-tags=factory-agent
# The agent VMs: inbound ssh from IAP only.
gcloud compute firewall-rules create factory-agent-ssh-iap --network=factory --direction=INGRESS \
  --priority=1000 --action=ALLOW --rules=tcp:22 --source-ranges=35.235.240.0/20 --target-tags=factory-agent
# The server VM: the three ports, from the agent VMs only.
gcloud compute firewall-rules create factory-server-from-agents --network=factory --direction=INGRESS \
  --priority=1000 --action=ALLOW --rules=tcp:3128,tcp:4647,tcp:7600 \
  --source-tags=factory-agent --target-tags=factory-server
# The server VM: webhooks (Caddy), and the Dispatcher's API from the SolidTrace VM.
gcloud compute firewall-rules create factory-server-webhooks --network=factory --direction=INGRESS \
  --priority=1000 --action=ALLOW --rules=tcp:443 --source-ranges=0.0.0.0/0 --target-tags=factory-server
gcloud compute firewall-rules create factory-server-from-solidtrace --network=factory --direction=INGRESS \
  --priority=1000 --action=ALLOW --rules=tcp:7600 --source-tags=solidtrace --target-tags=factory-server
# A service account with no roles, for the agent VMs. Do not grant it any role.
gcloud iam service-accounts create factory-agent --display-name="Factory agent VMs"
```

The VMs that you make from the image use `--service-account=factory-agent@PROJECT.iam.gserviceaccount.com --no-scopes`. The metadata server and its DNS stay reachable on Google Cloud when the rules deny all other egress. On another cloud, make the same rules with its tools.

## From an empty server VM to a first Run

You need: an Organization with Agent sessions turned on ([Agent sessions](agent-sessions.md)), where you are an Admin; an Instance admin for step 6; a Bitbucket Cloud repository and an access token for it; and a Claude subscription account for each account of the pool.

### 1. Make the VMs and the network

Make the server VM (Debian 12, 2 vCPU and 4 GB or more) and put the rules of the section above in place. Note the server VM's private address (here `10.100.0.3`). Install Docker with its Compose plugin on the server VM.

### 2. Install and set up Nomad

1. Install Nomad 2.1.1 or later from HashiCorp's packages. It is the version that the local stack's tests use. The package puts its own `/etc/nomad.d/nomad.hcl` in place, which turns on a Nomad server **and** a Nomad client, and binds to `0.0.0.0`. Nomad merges all the files of `/etc/nomad.d`, and a `enabled = false` in another file does not turn them off. Empty the file: `: > /etc/nomad.d/nomad.hcl`. Do this before you start Nomad, and again after each upgrade of the package, if the package puts the file back.
2. Copy [`nomad/server.hcl`](../factory/reference/nomad/server.hcl) to `/etc/nomad.d/server.hcl`, and set `PRIVATE_IP`. Start Nomad (`systemctl enable --now nomad`).
3. Bootstrap the ACLs, and keep the management token: `nomad acl bootstrap`.
4. Make the node pool of each machine kind. A file `standard.pool.hcl` has `node_pool "standard" { description = "The standard machine kind" }`. Then run `nomad node pool apply standard.pool.hcl`.
5. Make the Dispatcher's token with the policy [`nomad/dispatcher-policy.hcl`](../factory/reference/nomad/dispatcher-policy.hcl):

   ```sh
   nomad acl policy apply factory-dispatcher dispatcher-policy.hcl
   nomad acl token create -name factory-dispatcher -policy factory-dispatcher -type client
   ```

   The policy lets the token register and dispatch the jobs of the Executions, and read jobs, allocations and nodes. It lets the token do nothing else.

### 3. Install Squid

1. Install Squid (`apt install squid`).
2. Make the allowlist directory, which the Dispatcher's container writes (the user of the container is 10001):

   ```sh
   install -d -o 10001 -g 10001 -m 755 /etc/squid/factory
   install -d -m 755 /etc/solid-trace-factory
   cp factory/reference/squid/allowlist.txt /etc/solid-trace-factory/default-allowlist.txt
   cp factory/reference/squid/allowlist.txt /etc/squid/factory/allowlist.txt
   chown 10001:10001 /etc/squid/factory/allowlist.txt
   ```

   `default-allowlist.txt` is the default allowlist of the reference configuration. Edit it for your team. The Dispatcher writes `/etc/squid/factory/allowlist.txt` as this list, plus the hosts of SolidTrace, plus the extra hosts of each repository that a Project names. It changes the file when a Project or a factory file changes.
3. Copy [`squid/squid.conf`](../factory/reference/squid/squid.conf) to `/etc/squid/squid.conf`. Set `agent_net` to the subnet of the agent VMs. Set the `solidtrace_*` lines to the host and port of the ingest URL (`4443` with HTTPS, `4000` without), or delete them when the ingest uses port 80 or 443. The wrappers upload through Squid, and Squid refuses a request to a port that is not set here.
4. Make Squid reload when the allowlist changes: copy the two files of [`systemd/`](../factory/reference/systemd) to `/etc/systemd/system/`, then run `systemctl enable --now squid-allowlist.path`. Restart Squid.

### 4. Install Caddy

Install Caddy. Put [`caddy/Caddyfile`](../factory/reference/caddy/Caddyfile) in `/etc/caddy/Caddyfile`, with your public name. Caddy gets a TLS certificate for that name, and sends only `/webhooks/*` to the webhook listener (127.0.0.1:7601). Each other path gets 404. You can use your own load balancer with the same rule.

### 5. Make the Dispatcher's token in SolidTrace

An Admin of the Organization opens **Settings → API tokens & MCP**, and makes **The Dispatcher's token**. The token acts as that Admin. Keep it safe: it is the Dispatcher's only access to SolidTrace. When the Admin leaves the Organization, the token stops, and the factory stops until an Admin makes a new one.

### 6. Start the Dispatcher

1. Make a directory `/opt/solid-trace-factory` with [`docker-compose.yml`](../factory/reference/docker-compose.yml). Copy [`dispatcher.env.example`](../factory/reference/dispatcher.env.example) to `/etc/solid-trace-factory/dispatcher.env`, with mode 600.
2. Set the values: SolidTrace's URL, SolidTrace's **ingest URL** (`DISPATCHER_SOLIDTRACE_INGEST_URL`; it is `https://HOST:4443` on an install with HTTPS, `http://HOST:4000` on one without, and is not Console's URL), the Dispatcher's token (step 5), the Nomad address and token (step 2), the machine kinds, and the private listener's address. Make the encryption key with `openssl rand -base64 32`, and keep a copy of it away from the VM. With another key, the Dispatcher does not start.
3. Set `DISPATCHER_LISTEN` to the server VM's **private address** and a port, such as `10.100.0.3:7600`. Do not use a public address or `0.0.0.0`. The default, `127.0.0.1:7600`, is a safe address that no agent VM can reach.
4. Start it:

   ```sh
   cd /opt/solid-trace-factory
   SOLIDTRACE_VERSION=v0.23.0 docker compose up -d
   docker compose logs dispatcher
   docker compose exec dispatcher dispatcher version
   ```

   The log shows "the Dispatcher serves the private listener" with its address. With a missing or wrong value, the Dispatcher stops and names the value.
5. Check the API from the SolidTrace VM: `curl http://10.100.0.3:7600/api/v1/status`. It gives the version and says whether SolidTrace accepts the Dispatcher's token.

### 7. Turn on the factory in Console

An Instance admin opens **Settings → Instance**, and sets **Dispatcher URL** to the private URL that Console's server uses (here `http://10.100.0.3:7600`). Set **Dispatcher public webhook URL** (optional) to the public name of step 4. From now on, Console shows the Factory section of each Project, the Runs page, the Stop button and the Factory Integration provider. Clear the Dispatcher URL to turn them off again.

### 8. Add the subscription accounts

Each Execution uses one Claude subscription account of the pool. Add each account with the operator command, on the server VM. It stores the token encrypted:

```sh
claude setup-token                   # on a machine where you can sign in to the account; it prints a token
cd /opt/solid-trace-factory
docker compose exec dispatcher dispatcher accounts add alice@example.com    # paste the token, then press Enter
docker compose exec dispatcher dispatcher accounts list
docker compose exec dispatcher dispatcher accounts remove alice@example.com
```

`list` shows each account with its live Executions and its limit time, and never a token. A Run with no free account stays queued, and its page says why. The Dispatcher sees a change at its next look at the queue: you do not restart it.

### 9. Bake the agent VM image and start the agent VMs

1. Download the two archives of the release and `factory-SHA256SUMS`, and check them: `sha256sum -c factory-SHA256SUMS`.
2. On a Debian 12 build VM, as root: unpack `factory-bin-linux-amd64.tar.gz` into `/tmp/factory-bin`, unpack `factory-image-recipe.tar.gz` into a directory, and run the recipe there. The settings are in the [recipe's guide](../factory/image/README.md):

   ```sh
   FACTORY_PROXY_URL=http://10.100.0.3:3128 FACTORY_DISPATCHER_HOST=10.100.0.3 \
   FACTORY_BIN_DIR=/tmp/factory-bin ./agent-vm.sh
   ```

3. Install Docker and Nomad on the same image, and empty the package's `/etc/nomad.d/nomad.hcl` (`: > /etc/nomad.d/nomad.hcl`). If you leave it, the agent VM starts a Nomad server of its own, on `0.0.0.0`, beside the client. Make the image of the VM.
4. Make each agent VM from the image, with the tag `factory-agent` and the service account with no roles. Put [`nomad/client.hcl`](../factory/reference/nomad/client.hcl) at `/etc/nomad.d/client.hcl`, with the server's address and the node pool.
5. On the server VM, make an introduction token for the VM: `nomad node intro create -node-name VM_NAME -ttl 10m`. Put it in the file `client/intro_token.jwt` of the client's data directory, readable by root only. Do not put it in the metadata. The client registers, and `nomad node status` shows it as `ready` in its node pool.

The Dispatcher and the agent CLI of an agent VM must be of the same release, or the CLI at most three minor releases older. The Dispatcher refuses an older CLI with a message that names the minimum.

### 10. Connect the forge and a Project

Each person uses their own machine with `solidtrace login` first. An Admin does these steps:

```sh
solidtrace login https://solidtrace.example.com
solidtrace connections set bitbucket-main --kind bitbucket --dispatcher http://10.100.0.3:7600 < bitbucket-token-file
solidtrace projects set my-project --repository bitbucket.org/workspace/name --connection bitbucket-main
```

The first Dispatcher verb needs `--dispatcher`. Later verbs use the same URL. Each Project can also be set in Console, in the Project's **Factory** section ([Agent sessions](agent-sessions.md)). Add `.solidtrace/factory.toml` to the repository when the Run needs a setup command, checks or more hosts ([an example](../factory/reference/factory.toml)). It applies after a person merges it into the default branch.

### 11. Start the first Run

```sh
solidtrace start --project my-project "Add a test for the date parser"
```

The command prints the Run's id and its link in Console. On the Run's page, the state goes from queued to running. The Dispatcher starts a Nomad job. Nomad places it on an agent VM, and the wrapper starts the agent. When the agent is done, the Run shows its PR and its Evidence. If the Run stays queued, look at its reason on the Run's page, and at `docker compose logs dispatcher`.

## Scaling

Steps 1 to 11 make the agent VMs by hand. For more than a few Runs at one time, let the **Nomad Autoscaler** add and remove them. This part is optional, and the local factory stack has no autoscaling.

How it works:

- Each machine kind has one Nomad node pool and one managed instance group (MIG) of agent VMs.
- The Autoscaler looks at the memory that Nomad allocated in each node pool (the Nomad APM, the query `percentage-allocated_memory`). Each Run takes all the memory of its VM, so this is the share of busy VMs. The target is 70%: with all VMs busy, it adds VMs. With a few busy, it removes the VMs that are not busy.
- It removes only a VM that has no allocation (the `empty` node selector). It never ends a Run to save a VM.
- You set the minimum and the maximum number of VMs of each kind. The first team uses 1 and 10.
- A Run that Nomad cannot place on a VM (the pool is at its maximum, or Spot capacity is short) waits in the queue. Its page says "waiting for a VM". After 10 minutes, the Dispatcher writes one warning Log (file `dispatcher`, the message starts "a Run waited more than 10 minutes for a VM", with the attributes `factory.run` and `factory.execution`). Make a Monitor on it.

### The instance template

The template is the same for Spot and standard VMs, except for the provisioning model. The Dispatcher, the wrapper and the Runs are the same on both. The example makes a Spot template. Change the image, the machine type, the network and the project:

```sh
gcloud compute instance-templates create factory-agent-standard-spot \
  --machine-type=e2-custom-4-12288 --image=factory-agent-image --image-project=PROJECT \
  --network=factory --subnet=factory --no-address --tags=factory-agent \
  --service-account=factory-agent@PROJECT.iam.gserviceaccount.com --no-scopes \
  --provisioning-model=SPOT --instance-termination-action=DELETE --preemption-notice-duration=120s
```

- **Spot:** `--provisioning-model=SPOT`, with the termination action `DELETE` and a notice of 120 seconds (`--preemption-notice-duration=120s`; `preemptionNoticeDuration` in the REST API). Google takes the VM back after the notice. The wrapper sees the notice, saves the Run's work, and a new Execution on a new VM continues it.
- **Standard:** `--provisioning-model=STANDARD`, and leave out the termination action and the notice. Use it when Google takes back Spot VMs too often. The wrapper still saves the work every 5 minutes.
- The metadata and the startup script hold no secret (the section "The network"). Each new VM needs an introduction token for its Nomad client before the client starts (step 9, item 5). A group cannot ask you for one: give the token with your own infrastructure code, in the client's root-only file, never in the metadata. This reference configuration does not do it for you.

Make one MIG for each kind, from its template. Do not turn on Google's own autoscaler for it: the Nomad Autoscaler sets the size.

```sh
gcloud compute instance-groups managed create factory-agents-standard \
  --template=factory-agent-standard-spot --size=1 --zone=europe-west1-b
```

### Change between Spot and standard

1. Make a second template, with the other provisioning model.
2. Point the group at it, with an opportunistic update, so that Google does not replace a VM that works on a Run:

   ```sh
   gcloud compute instance-groups managed rolling-action start-update factory-agents-standard \
     --version=template=factory-agent-standard --type=opportunistic --zone=europe-west1-b
   ```

3. Each VM that the group makes from now on is of the new model. The Autoscaler removes the old VMs when they are idle, and makes new ones when it needs them.

### Set up the Autoscaler

1. Make its Nomad token on the server VM with the policy [`nomad/autoscaler-policy.hcl`](../factory/reference/nomad/autoscaler-policy.hcl):

   ```sh
   nomad acl policy apply factory-autoscaler autoscaler-policy.hcl
   nomad acl token create -name factory-autoscaler -policy factory-autoscaler -type client
   ```

2. Give the server VM's service account the right to resize the groups and to delete their VMs (for example `roles/compute.instanceAdmin.v1` on the project). The Autoscaler reads its credentials from the metadata server.
3. Copy [`autoscaler/autoscaler.hcl`](../factory/reference/autoscaler/autoscaler.hcl) to `/etc/nomad-autoscaler/autoscaler.hcl` and set the server VM's address. Copy [`autoscaler/policies/standard.hcl`](../factory/reference/autoscaler/policies/standard.hcl) to `/etc/nomad-autoscaler/policies/`, one file for each machine kind: change the name of the policy, `node_pool` (the kind's name), `mig_name`, `project` and `zone`.
4. Put the line `NOMAD_TOKEN=` and the token in `/etc/solid-trace-factory/autoscaler.env` (owner root, mode 600). Start it with [`autoscaler/docker-compose.yml`](../factory/reference/autoscaler/docker-compose.yml): `docker compose -f docker-compose.yml up -d`.
5. Look at `docker compose logs`. A policy that the Autoscaler refuses is in the log, with the reason.

### Set the minimum and the maximum

The `min` and `max` of the policy file of each kind are the minimum and the maximum number of VMs of that kind. Change them, and restart the Autoscaler (`docker compose restart autoscaler`).

- A minimum of 1 keeps one VM ready, so that the first Run does not wait for a VM to start (about two minutes). A minimum of 0 costs nothing when idle, and the first Run waits.
- Runs over the maximum wait in the queue, and give the warning Log after 10 minutes. If you see the warning often, raise the maximum.
- Keep the maximum under what your Google Cloud quota allows.

### The fallback policy

If the Nomad APM does not scale a group in your cluster (cluster check 4 of the build fails), use [`autoscaler/fallback/standard.hcl`](../factory/reference/autoscaler/fallback/standard.hcl) in place of the main policy. It reads the busy VMs from Prometheus, with the pass-through strategy. It needs Nomad's metrics in your Prometheus, and the `apm "prometheus"` block of `autoscaler.hcl`.

## Operate

- **Upgrade:** set `SOLIDTRACE_VERSION` to the new release and run `docker compose pull && docker compose up -d`. The Dispatcher changes its database by itself. Bake a new agent VM image with the same release's archives.
- **Back up:** the Docker volume `solid-trace-factory_dispatcher-data` (the SQLite file) and the encryption key. Without the key, the secrets in the database are lost.
- **Stop all work:** stop a Run with `solidtrace stop RUN_ID`, or the whole factory with `docker compose stop`.
- **Turn the factory off:** clear the Dispatcher URL in Settings → Instance, and stop the Dispatcher. A SolidTrace install is as it was before.
- **Logs:** the Dispatcher writes its own events as Logs in the Agent Project (file `dispatcher`), and its process log to `docker compose logs dispatcher`.

## The settings

[`dispatcher.env.example`](../factory/reference/dispatcher.env.example) lists each setting of the Dispatcher with its meaning. `dispatcher accounts` and `dispatcher version` are the two commands of the binary, and the image's entry point runs them.
