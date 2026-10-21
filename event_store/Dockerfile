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

FROM golang:1.25-bookworm AS builder
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

# Build binary. DuckDB, SQLite and the VRL runtime are cgo libraries.
RUN CGO_ENABLED=1 go build -o event_store .

# DuckDB's httpfs extension, which reads Log files from an object_store
# bucket. With an object_store, EventStore loads it from this directory and
# never lets DuckDB download an extension itself. The version is DuckDB's in
# duckdb-go (go.mod); change both together (a storage test checks they match).
# DuckDB checks the file's signature when it loads it.
FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS extensions
ARG TARGETARCH
ARG DUCKDB_VERSION=v1.5.6
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*
RUN dir=/usr/local/lib/solidtrace/duckdb_extensions/${DUCKDB_VERSION}/linux_${TARGETARCH} \
    && mkdir -p "$dir" \
    && curl -fsSL -o /tmp/httpfs.gz "https://extensions.duckdb.org/${DUCKDB_VERSION}/linux_${TARGETARCH}/httpfs.duckdb_extension.gz" \
    && gunzip -c /tmp/httpfs.gz > "$dir/httpfs.duckdb_extension" \
    && rm /tmp/httpfs.gz

# Final stage
FROM debian:bookworm-slim

WORKDIR /app

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    libsqlite3-0 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=extensions /usr/local/lib/solidtrace/duckdb_extensions /usr/local/lib/solidtrace/duckdb_extensions
COPY --from=builder /app/event_store .

EXPOSE 4000

CMD ["./event_store"]
