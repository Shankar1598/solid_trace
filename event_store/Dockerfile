# The Log transform's VRL runtime: the Rust vrl crate built as a static
# library, which the builder links (docs/adr/0012-log-transform-runs-vrl-through-cgo.md).
# Bookworm, as the builder and the final image are, so the library's glibc
# symbols resolve. Keep Rust's version in step with mise.toml.
FROM rust:1.99-slim-bookworm AS vrl

WORKDIR /vrl

# The dependencies build on their own first, so a change to the crate's code
# reuses them.
COPY vrl/rust/Cargo.toml vrl/rust/Cargo.lock ./
RUN mkdir src && touch src/lib.rs \
    && cargo build --profile dist --locked \
    && rm -r src

COPY vrl/rust/src src
# The copied code can be older than the stand-in built above, so touch it
# for Cargo to rebuild the crate.
RUN touch src/lib.rs && cargo build --profile dist --locked

FROM golang:1.27-bookworm AS builder
ARG TARGETARCH

WORKDIR /app

# Install build dependencies
RUN apt-get update && apt-get install -y \
    gcc \
    g++

COPY go.mod go.sum ./
RUN go mod download

# Where vrl/build puts the library for the vrl package to link.
COPY --from=vrl /vrl/target/dist/libst_vrl.a vrl/lib/linux_${TARGETARCH}/libst_vrl.a

COPY . .

# Build binary. SQLite and the VRL runtime are cgo libraries.
RUN CGO_ENABLED=1 go build -o event_store .

# Final stage
FROM debian:bookworm-slim

WORKDIR /app

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    libsqlite3-0 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/event_store .

# The release, which prepare-clickhouse's marker names. The release workflow
# sets it to the tag.
ARG SOLIDTRACE_VERSION=dev
ENV SOLIDTRACE_VERSION=$SOLIDTRACE_VERSION

EXPOSE 4000

CMD ["./event_store"]
