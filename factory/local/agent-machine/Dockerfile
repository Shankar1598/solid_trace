# The first agent machine of the local factory stack. Ticket 46 makes this the image
# recipe for agent VMs. It has the run user, the one sudoers rule, hc, Claude Code at
# the pinned version with its first-run dialogs accepted, and the proxy settings.
#
# It has no Nomad binary: Nomad is not shipped (ADR 0019). The stack mounts the binary
# from Nomad's image, which only the local stack's compose file names.
#
# It has no credentials of any kind. A Run gets its Claude token from the Dispatcher.
FROM debian:bookworm-slim

# The pinned versions, with the SHA-256 of each download (linux x86_64).
ARG CLAUDE_CODE_VERSION=2.1.325
ARG CLAUDE_CODE_SHA256=0dc4f4a9a165fc60746d7ae8489712e5fc352f1e7ad4eb22e7141bd0812eb0cf
ARG HC_VERSION=0.1.21
ARG HC_SHA256=78424755c10a40dde38a0bdb55c20ac19706e9ae05ed68fc2acf1fcff0d941c1

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl git sudo procps \
 && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL -o /usr/local/bin/claude \
      "https://downloads.claude.ai/claude-code-releases/${CLAUDE_CODE_VERSION}/linux-x64/claude" \
 && echo "${CLAUDE_CODE_SHA256}  /usr/local/bin/claude" | sha256sum -c - \
 && chmod 755 /usr/local/bin/claude

RUN curl -fsSL -o /tmp/hc.tar.gz \
      "https://releases.herdr.dev/core/${HC_VERSION}/herdr-core-linux-x86_64.tar.gz" \
 && echo "${HC_SHA256}  /tmp/hc.tar.gz" | sha256sum -c - \
 && tar -xzf /tmp/hc.tar.gz -C /usr/local/bin herdr-core \
 && rm /tmp/hc.tar.gz \
 && chown root:root /usr/local/bin/herdr-core \
 && chmod 755 /usr/local/bin/herdr-core \
 && ln -s herdr-core /usr/local/bin/hc

# The run user (not root, in the docker group) and the access group. "engineer" is a
# member of the access group on the local stack only: on a VM, OS Login gives the
# group to each person of the Google group.
RUN groupadd --system docker \
 && groupadd --system factory-access \
 && useradd --create-home --shell /bin/bash --groups docker agent \
 && useradd --create-home --shell /bin/bash --groups factory-access engineer \
 && install -d -o agent -g agent -m 755 /work

COPY sudoers /etc/sudoers.d/factory-take-over
RUN chmod 440 /etc/sudoers.d/factory-take-over && visudo -c

# Claude Code's first-run dialogs, accepted in files that root owns. The wrapper deletes
# the run user's home after each Execution and sets CLAUDE_CONFIG_DIR, so files in the
# home do not apply to a Run.
# - Claude Code's managed settings, which the run user cannot change: no bypass-mode
#   warning, and no auto-updater, so that the pinned version stays.
# - The template of the run user's .claude.json, with the onboarding done. For each
#   Execution the wrapper copies it to $CLAUDE_CONFIG_DIR/.claude.json and adds
#   projects["<checkout>"].hasTrustDialogAccepted for its checkout: Claude Code trusts
#   a checkout only at its exact path (a trusted parent directory does not count).
COPY managed-settings.json /etc/claude-code/managed-settings.json
COPY claude.json /etc/factory/claude.json

# The proxy settings. Tools read HTTPS_PROXY; git over ssh is rewritten to HTTPS.
ENV HTTPS_PROXY=http://squid:3128 \
    HTTP_PROXY=http://squid:3128 \
    https_proxy=http://squid:3128 \
    http_proxy=http://squid:3128 \
    NO_PROXY=localhost,127.0.0.1 \
    no_proxy=localhost,127.0.0.1 \
    DISABLE_AUTOUPDATER=1
RUN printf '%s\n' \
      'HTTPS_PROXY=http://squid:3128' 'HTTP_PROXY=http://squid:3128' \
      'https_proxy=http://squid:3128' 'http_proxy=http://squid:3128' \
      'NO_PROXY=localhost,127.0.0.1' 'no_proxy=localhost,127.0.0.1' \
      'DISABLE_AUTOUPDATER=1' >>/etc/environment \
 && git config --system url."https://github.com/".insteadOf "git@github.com:" \
 && git config --system --add url."https://github.com/".insteadOf "ssh://git@github.com/" \
 && git config --system url."https://bitbucket.org/".insteadOf "git@bitbucket.org:" \
 && git config --system --add url."https://bitbucket.org/".insteadOf "ssh://git@bitbucket.org/"

COPY client.hcl /etc/nomad.d/client.hcl
COPY entrypoint.sh /usr/local/bin/agent-machine
RUN chmod 755 /usr/local/bin/agent-machine && install -d -m 700 /var/lib/nomad

CMD ["/usr/local/bin/agent-machine"]
