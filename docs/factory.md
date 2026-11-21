# The Factory: operator guide

The **Factory** gives work to coding agents on your own machines. A person, a tracker ticket, a schedule or a webhook starts a **Run**. The **Dispatcher** queues the Run and starts its **Executions**. Each Execution works on its own agent VM. SolidTrace keeps the record of each Run.

This guide goes from an empty server VM to a first Run. Read [Agent sessions](agent-sessions.md) for what people do with Runs.

## What you install

| Part | Where | What it is |
|---|---|---|
| The Dispatcher | The server VM | One container, from the release's image `ghcr.io/shankar1598/solid-trace-dispatcher`, for amd64 and arm64. It keeps its state in one SQLite file. |
| Nomad | The server VM and the agent VMs | One server and one client for each agent VM. Nomad is not in any SolidTrace image (its licence is BSL). You install it from HashiCorp's packages. |
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

- **Never install the Dispatcher on the SolidTrace VM.** Agents use much CPU and memory. They must not slow the ingest of Logs. The factory has its own server VM, beside the Nomad server.
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

## Operate

- **Upgrade:** set `SOLIDTRACE_VERSION` to the new release and run `docker compose pull && docker compose up -d`. The Dispatcher changes its database by itself. Bake a new agent VM image with the same release's archives.
- **Back up:** the Docker volume `solid-trace-factory_dispatcher-data` (the SQLite file) and the encryption key. Without the key, the secrets in the database are lost.
- **Stop all work:** stop a Run with `solidtrace stop RUN_ID`, or the whole factory with `docker compose stop`.
- **Turn the factory off:** clear the Dispatcher URL in Settings → Instance, and stop the Dispatcher. A SolidTrace install is as it was before.
- **Logs:** the Dispatcher writes its own events as Logs in the Agent Project (file `dispatcher`), and its process log to `docker compose logs dispatcher`.

## The settings

[`dispatcher.env.example`](../factory/reference/dispatcher.env.example) lists each setting of the Dispatcher with its meaning. `dispatcher accounts` and `dispatcher version` are the two commands of the binary, and the image's entry point runs them.
