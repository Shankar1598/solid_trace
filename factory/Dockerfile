# The Dispatcher's image (docs/factory.md). It is the factory's one service, on the
# server VM beside the Nomad server. A SolidTrace install (deploy/) does not run it.
#
# The build context is the repository root, because the factory module imports the
# agent CLI's packages (factory/go.mod replaces them with ../cli). The release job
# builds it with:
#   docker build -f factory/Dockerfile --build-arg SOLIDTRACE_VERSION=v0.23.0 .
# Nomad is not in the image (its licence is BSL, ADR 0019): the Dispatcher calls
# Nomad's HTTP API.
FROM golang:1.27-bookworm AS builder

WORKDIR /src

# The modules download first, so a change to the code reuses them. The factory
# module's replace directive needs the CLI's go.mod beside it.
COPY cli/go.mod ./cli/go.mod
COPY factory/go.mod factory/go.sum ./factory/
WORKDIR /src/factory
RUN go mod download

COPY cli /src/cli
COPY factory /src/factory

# SQLite is a cgo library, so the build needs gcc (golang's image has it).
ARG SOLIDTRACE_VERSION=dev
RUN CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.Version=${SOLIDTRACE_VERSION}" \
    -o /out/dispatcher ./cmd/dispatcher

# Final stage. git: the Dispatcher deletes the work-in-progress refs of a Closed Run
# with it. sh: the command that reloads Squid.
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    git \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --home-dir /var/lib/dispatcher --shell /usr/sbin/nologin dispatcher \
    && mkdir -p /var/lib/dispatcher \
    && chown dispatcher:dispatcher /var/lib/dispatcher

COPY --from=builder /out/dispatcher /usr/local/bin/dispatcher

# The SQLite database is in this directory: keep it in a volume. The rest of the
# operator configuration comes from the environment or a file (docs/factory.md).
ENV DISPATCHER_DATABASE=/var/lib/dispatcher/dispatcher.sqlite3
VOLUME /var/lib/dispatcher
WORKDIR /var/lib/dispatcher
USER dispatcher

# The private listener's port is the operator's: DISPATCHER_LISTEN (default
# 127.0.0.1:7600) names its address, so the image exposes no port.
ENTRYPOINT ["dispatcher"]
