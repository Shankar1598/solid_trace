# SolidTrace Load Testing

This directory contains k6 load test scripts for benchmarking SolidTrace's event ingestion pipeline.

## Prerequisites

### Install k6

```bash
# macOS
brew install k6

# Linux (Debian/Ubuntu)
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update
sudo apt-get install k6

# Docker
docker pull grafana/k6
```

### Start Services

Make sure all SolidTrace services are running:

```bash
bin/dev
```

This starts:
- Rails app on port 3000
- Event Store (Go) on port 4000
- Vite dev server on port 5173
- Background job processor

## Quick Start

The easiest way to run load tests is using the automation script:

```bash
# Run with defaults (single_issue scenario, 100 VUs, 2 minutes)
bin/load-test

# Specify scenario
bin/load-test --scenario per_event

# Customize VUs and duration
bin/load-test --scenario per_100 --vus 200 --duration 5m

# Skip database reset (faster, use existing data)
bin/load-test --scenario single_issue --skip-reset
```

## Test Scenarios

The load test measures event ingestion throughput with different issue creation patterns:

| Scenario | Description | Use Case |
|----------|-------------|----------|
| `single_issue` | All events grouped into one issue (same fingerprint) | Best-case: single hot issue receiving all traffic |
| `per_1000` | New issue created every 1000 events | Moderate: issues created occasionally |
| `per_100` | New issue created every 100 events | Frequent: new issues every few seconds |
| `per_event` | Every event creates a new issue (unique fingerprints) | Worst-case: maximum write amplification |

### Fingerprint Behavior

Events are grouped into issues based on their fingerprint. The scenario controls how fingerprints are generated:

- **single_issue**: Static fingerprint → all events aggregate to 1 issue
- **per_1000**: Fingerprint rotates every 1000 events → ~1 new issue per second at 1000 RPS
- **per_100**: Fingerprint rotates every 100 events → ~10 new issues per second at 1000 RPS
- **per_event**: Unique fingerprint per event → 1000 new issues per second at 1000 RPS

## Running Tests Manually

If you prefer to run k6 directly:

```bash
# Basic run
k6 run test/load/k6/event_ingestion.js

# With environment variables
k6 run \
  -e SCENARIO=per_event \
  -e MAX_VUS=200 \
  -e DURATION=5m \
  -e RAMP_UP=1m \
  -e EVENT_STORE_URL=http://localhost:4000 \
  -e PROJECT_ID=1 \
  -e PUBLIC_KEY=testkey123 \
  test/load/k6/event_ingestion.js
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SCENARIO` | `single_issue` | Test scenario (see above) |
| `MAX_VUS` | `100` | Maximum virtual users (concurrency) |
| `DURATION` | `2m` | Duration at max VUs |
| `RAMP_UP` | `30s` | Ramp-up time per stage (4 stages total) |
| `RAMP_DOWN` | `10s` | Graceful ramp-down time |
| `EVENT_STORE_URL` | `http://localhost:4000` | Event Store endpoint |
| `PROJECT_ID` | `1` | Project ID for API calls |
| `PUBLIC_KEY` | `testkey123` | API key (from db/seeds.rb) |

## VU Calibration Guide

The goal is to find the maximum sustainable RPS for your hardware. Follow this process:

### Step 1: Start Small

```bash
bin/load-test --scenario single_issue --vus 50 --duration 1m
```

Observe:
- **RPS** (`http_reqs` rate): Current throughput
- **P95 Latency**: Should stay under 100ms for healthy load
- **Success Rate**: Should be > 99%

### Step 2: Increase Gradually

Double VUs until you see degradation:

```bash
bin/load-test --scenario single_issue --vus 100 --duration 1m --skip-reset
bin/load-test --scenario single_issue --vus 200 --duration 1m --skip-reset
bin/load-test --scenario single_issue --vus 400 --duration 1m --skip-reset
```

### Step 3: Identify Saturation Point

Watch for these signs of saturation:
- **RPS plateaus**: Increasing VUs no longer increases throughput
- **Latency spikes**: P95/P99 jumps significantly (e.g., 100ms → 500ms+)
- **Errors appear**: Success rate drops below 99%
- **503 responses**: Event Store returns backpressure errors

### Step 4: Fine-tune

Once you find the approximate limit, narrow down:

```bash
# If 200 VUs was good but 400 showed degradation:
bin/load-test --scenario single_issue --vus 250 --skip-reset
bin/load-test --scenario single_issue --vus 300 --skip-reset
```

### Step 5: Run Full Benchmark

With your optimal VU count, run a longer test:

```bash
bin/load-test --scenario single_issue --vus 250 --duration 5m
```

## Interpreting Results

After each test, k6 outputs a summary. Key metrics to watch:

```
     ✓ status is 200

     checks.........................: 100.00% ✓ 150000     ✗ 0
     data_received..................: 15 MB   250 kB/s
     data_sent......................: 1.2 GB  20 MB/s
     events_ingested................: 150000  2500/s       ← Total events sent
     http_req_duration..............: avg=12ms min=2ms med=8ms max=250ms p(90)=25ms p(95)=45ms
     http_reqs......................: 150000  2500/s       ← THIS IS YOUR RPS
     success_rate...................: 100.00% ✓ 150000     ✗ 0
```

### Metrics Explained

| Metric | Description | Target |
|--------|-------------|--------|
| `http_reqs` (rate) | Requests per second - **your max RPS** | Higher is better |
| `http_req_duration` p95 | 95th percentile latency | < 100ms |
| `http_req_duration` p99 | 99th percentile latency | < 500ms |
| `success_rate` | Percentage of successful requests | > 99% |
| `events_ingested` | Total events successfully stored | - |
| `payload_size_bytes` | Average event payload size | ~8-10KB |

## Payload Details

The test generates realistic Sentry-compatible event payloads (~8-10KB each):

- **Exception**: Full error with type, message, and mechanism
- **Stacktrace**: 20-30 frames with file paths, functions, line numbers, and context
- **Breadcrumbs**: 10-15 entries showing user journey
- **Contexts**: Browser, OS, device, runtime, and trace information
- **User**: ID, email, IP address, and geo data
- **Tags**: Environment, server, region, version metadata
- **Extra**: Request ID, feature flags, performance metrics

## Troubleshooting

### "Too many open files" Error

Increase the file descriptor limit:

```bash
ulimit -n 65536
```

Add to `~/.zshrc` or `~/.bashrc` for persistence.

### Event Store Not Responding

1. Check if services are running: `bin/dev`
2. Verify Event Store health: `curl http://localhost:4000/health`
3. Check logs: `tail -f log/development.log`

### Low RPS Despite High VUs

- **CPU bound**: Check if Event Store or database is at 100% CPU
- **Disk I/O**: SQLite/DuckDB writes may be the bottleneck on slower disks
- **Network**: Unlikely for localhost, but check if using remote services

### High Error Rate

- **503 errors**: Event Store backpressure - reduce VUs or increase channel sizes
- **401 errors**: Check `PUBLIC_KEY` matches `db/seeds.rb`
- **Connection errors**: Services may have crashed - restart with `bin/dev`

## Running All Scenarios

To benchmark all scenarios and compare:

```bash
#!/bin/bash
for scenario in single_issue per_1000 per_100 per_event; do
  echo "Running $scenario..."
  bin/load-test --scenario $scenario --vus 100 --duration 2m
  sleep 10  # Cool-down between tests
done
```

## File Structure

```
test/load/k6/
├── event_ingestion.js    # Main test script
├── lib/
│   ├── config.js         # Shared configuration
│   └── payloads.js       # Event payload generator
└── README.md             # This file
```
