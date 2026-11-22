# The agent VM image recipe

[agent-vm.sh](agent-vm.sh) is the recipe for the image of an agent VM. You bake it with your own image pipeline (such as Packer, or a startup step of your image build) on Debian 12. The local factory stack's agent machine is made from the same recipe ([../local/agent-machine/Dockerfile](../local/agent-machine/Dockerfile)).

## What it installs

| Part | Where | Version |
|---|---|---|
| Claude Code | `/usr/local/bin/claude` | pinned in the recipe (`CLAUDE_CODE_VERSION`), checked by its SHA-256 |
| `hc` (Herdr core) | `/usr/local/bin/hc`, a link to `herdr-core` | pinned in the recipe (`HC_VERSION`), checked by its SHA-256 |
| The wrapper | `/usr/local/bin/factory-wrapper` (the Nomad Runner's default command) | the version that `build` gives it (`factory-wrapper version`) |
| The agent CLI | `/usr/local/bin/solidtrace` | the version that `build` gives it (`solidtrace --version`) |
| Claude Code's first-run dialogs, accepted | `/etc/claude-code/managed-settings.json` and the template `/etc/factory/claude.json`, owned by root | |
| The run user `agent`: not root, in the `docker` group, with no password | | |
| The access group (default `factory-access`), with no member | | |
| The one sudoers rule | `/etc/sudoers.d/factory-take-over` | |
| git: no credential helper of the system, and git over ssh to GitHub and Bitbucket rewritten to HTTPS | `/etc/gitconfig` | |
| The proxy settings | `/etc/factory/proxy.env`, `/etc/environment`, and a systemd drop-in for `docker.service` and `nomad.service` | |
| The Nomad client's settings for the Runner's job: `raw_exec` on, `max_kill_timeout = "120s"` | `/etc/nomad.d/factory.hcl` | |
| The directory of the Executions | `/var/lib/factory/executions`, mode 711, owned by root | |

It does not install Nomad (its licence is BSL, [ADR 0019](../../docs/adr/0019-agents-run-on-nomad-and-the-factory-is-a-separable-module.md)) or Docker. Your pipeline installs them (and empties the Nomad package's `/etc/nomad.d/nomad.hcl`, which turns on a Nomad server), and the operator's own Nomad client file in `/etc/nomad.d` gives the servers, the node pool and the introduction token. The recipe makes the `docker` group, and the Docker package uses it.

The image has no credential of any kind. A Run gets its tokens from the Dispatcher. The git credential helper of each Execution is a file that the wrapper writes in the Execution's secrets directory, for the forge's URL only; the image has no helper of its own.

## Bake it

```sh
factory/image/build /tmp/factory-bin v0.23.0      # the wrapper and the agent CLI, static, Linux x86_64
# Copy factory/image and /tmp/factory-bin to the build VM, then, as root:
FACTORY_PROXY_URL=http://10.100.0.3:3128 \
FACTORY_DISPATCHER_HOST=10.100.0.3 \
FACTORY_BIN_DIR=/tmp/factory-bin \
FACTORY_ACCESS_GROUP=factory-access \
  ./agent-vm.sh
```

| Setting | Meaning |
|---|---|
| `FACTORY_PROXY_URL` | Squid on the server VM, as `http://<host>:<port>`. Required. No user name or password. |
| `FACTORY_DISPATCHER_HOST` | The host of the Dispatcher's private listener. Tools reach it direct, not through the proxy (Squid refuses its port). Required. |
| `FACTORY_BIN_DIR` | The directory with `factory-wrapper` and `solidtrace`. Required. |
| `FACTORY_ACCESS_GROUP` | The POSIX group that OS Login gives to each person of your Google group. Default `factory-access`. |
| `FACTORY_DOWNLOAD_CACHE` | A directory that keeps the downloads for the next run. |

The run user is always `agent`, the wrapper's default (`WRAPPER_RUN_USER`, and `WRAPPER_HOME` `/home/agent`). Nothing gives another name to the wrapper, so the recipe refuses `FACTORY_RUN_USER` with another name, and a user `agent` with the user id 0.

The recipe checks each setting before it changes anything, and refuses a name or a value that could change the sudoers rule or an environment file. A second run changes nothing. An argument `system` or `factory` runs only one part: `system` is all but the two binaries of this repository, and `factory` installs them.

`NO_PROXY` has `localhost`, `127.0.0.1`, the metadata server (`169.254.169.254`, `metadata.google.internal`) and the Dispatcher's host.

To change a pinned version, change the version and its SHA-256 at the top of `agent-vm.sh`.

## The take-over rule

```
%factory-access ALL=(agent) NOPASSWD: /usr/local/bin/hc connect
```

A member of the access group runs exactly `sudo -u agent hc connect`, with no password, and nothing else: no other command, no other argument, no other user, no `-E`, no shell. The run user and each other user have no sudo rule. This gives the person the run user's rights, and root on that VM through the `docker` group; that is accepted, because the VM holds one Run. Attach only to a core that the wrapper started: when no core runs, `hc connect` starts one of its own, and that core gets the `SUDO_*` variables.

The rule is the one sudoers rule only when two more things are true:

- Give the Google group the role `roles/compute.osLogin` on the agent VMs, and never `roles/compute.osAdminLogin`. OS Login gives general sudo to each person with the admin role, through sudoers files of its own.
- The base image has no other sudoers rule. At the end of its sudoers step, the recipe fails with a message that starts "no general sudo" when `/etc/sudoers.d` has a file other than `README` and `factory-take-over` (for example, a rule that a cloud guest agent wrote for the user of the image build), or when the group `sudo` has a member, in its member list or as a user's primary group (Debian's `/etc/sudoers` gives that group all commands). Remove that file or member from the base image, and run the recipe again.

## Test it

```sh
factory/image/test        # the recipe in a clean Debian container
factory/local/smoke       # the local stack, whose agent machine the recipe made
```

`test` builds the wrapper and the agent CLI, runs the recipe in a clean Debian container with the run user `agent` and an access group that is not the default, and checks the refusals of bad settings and of a download with another SHA-256, a second run, the versions, the owners and modes, the sudoers rule and what it refuses, the refusal of a base image with another sudo rule, the dialogs, the proxy settings and git. It keeps the downloads in the Docker volume `factory-image-test-downloads`; `FACTORY_IMAGE_TEST_CLEAN=1` deletes it. An agent runs each of the two through `~/.cache/solid-trace/software-factory-notes/slot.sh exclusive`.
