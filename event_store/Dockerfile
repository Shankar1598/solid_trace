FROM golang:1.25-bookworm AS builder

WORKDIR /app

# Install build dependencies
RUN apt-get update && apt-get install -y \
    gcc \
    g++

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build binary. DuckDB and SQLite are cgo libraries, and DuckDB links the
# 2.0 alpha shared library fetched by scripts/fetch-duckdb.
RUN scripts/fetch-duckdb
RUN CGO_ENABLED=1 \
    GOFLAGS=-tags=duckdb_use_lib \
    CGO_LDFLAGS="-L/app/lib/duckdb" \
    go build -o event_store .

# Final stage
FROM debian:bookworm-slim

WORKDIR /app

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    libsqlite3-0 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/lib/duckdb/libduckdb.so /usr/local/lib/
RUN ldconfig

COPY --from=builder /app/event_store .

EXPOSE 4000

CMD ["./event_store"]
