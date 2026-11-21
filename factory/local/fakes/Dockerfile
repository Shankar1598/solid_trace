# The fake Forge and the fake Tracker of the local factory stack (factory/internal/localfakes).
# "stack up" with FACTORY_STACK_DISPATCHER=1 first builds the program into bin/.
# git: the fake Forge serves its bare repositories with git-http-backend.
# curl: an agent that checks the stack reads the fake Forge's API from inside this container.
FROM debian:bookworm-20261104-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc

RUN apt-get update \
 && apt-get install -y --no-install-recommends git curl ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 10002 --create-home --home-dir /var/lib/fakes fakes \
 && git config --system user.name "A person" \
 && git config --system user.email person@example.com

COPY bin/localfakes /usr/local/bin/localfakes
COPY test-repo /etc/fakes/test-repo

ENV FAKES_REPO_FILES=/etc/fakes/test-repo
USER fakes
CMD ["/usr/local/bin/localfakes"]
