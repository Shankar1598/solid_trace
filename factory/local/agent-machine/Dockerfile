# The agent machine of the local factory stack, made by the agent VM image recipe
# (factory/image/agent-vm.sh), as an image pipeline makes an agent VM. The recipe has
# the pinned versions, the run user, the one sudoers rule, hc, Claude Code with its
# first-run dialogs accepted, the wrapper, the agent CLI and the proxy settings.
#
# Build it with two contexts: this directory, with bin/ from factory/image/build, and
# the recipe's directory as "image". factory/local/stack does both:
#
#   factory/image/build factory/local/agent-machine/bin
#   docker build --build-context image=factory/image factory/local/agent-machine
#
# It has no Nomad binary: Nomad is not shipped (ADR 0019). The stack mounts the binary
# from Nomad's image, which only the local stack's compose file names.
#
# It has no credentials of any kind. A Run gets its Claude token from the Dispatcher.
#
# Two targets: agent-vm (the image recipe only) and local (agent-vm with the local
# stack's take-over user and its Nomad client). The local stack's compose file builds
# local.
FROM debian:bookworm-20261104-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc AS agent-vm

# The recipe's settings for the local stack: Squid and the Dispatcher are compose services.
ENV FACTORY_PROXY_URL=http://squid:3128 \
    FACTORY_DISPATCHER_HOST=dispatcher

# Two layers, so that a change to the wrapper or the agent CLI does not download Claude
# Code again. The cache keeps the downloads for the next build.
RUN --mount=type=bind,from=image,target=/opt/factory-image \
    --mount=type=cache,target=/var/cache/factory-image \
    FACTORY_DOWNLOAD_CACHE=/var/cache/factory-image /opt/factory-image/agent-vm.sh system
RUN --mount=type=bind,source=bin,target=/opt/factory-bin \
    --mount=type=bind,from=image,target=/opt/factory-image \
    FACTORY_BIN_DIR=/opt/factory-bin /opt/factory-image/agent-vm.sh factory

# A container has no systemd and no PAM login, so the proxy settings of the recipe's
# /etc/factory/proxy.env are also the image's environment, for "docker exec". The
# entrypoint gives the file to the Nomad client, as systemd does on a VM. The smoke
# check compares the two.
ENV HTTPS_PROXY=http://squid:3128 \
    HTTP_PROXY=http://squid:3128 \
    https_proxy=http://squid:3128 \
    http_proxy=http://squid:3128 \
    NO_PROXY=localhost,127.0.0.1,169.254.169.254,metadata.google.internal,dispatcher \
    no_proxy=localhost,127.0.0.1,169.254.169.254,metadata.google.internal,dispatcher \
    DISABLE_AUTOUPDATER=1

CMD ["/bin/bash"]

# The local stack only: "engineer", a member of the access group, attaches with
# "docker exec" in place of ssh and OS Login. The agent-vm target has no such user.
# The Nomad client's own settings (the server, the introduction token) and the
# entrypoint, which prepares cgroup v2 as a VM has it.
FROM agent-vm AS local
RUN useradd --create-home --shell /bin/bash --groups factory-access engineer \
 && install -d -o agent -g agent -m 755 /work \
 && install -d -m 700 /var/lib/nomad
COPY client.hcl /etc/nomad.d/client.hcl
COPY entrypoint.sh /usr/local/bin/agent-machine
RUN chmod 755 /usr/local/bin/agent-machine
CMD ["/usr/local/bin/agent-machine"]
