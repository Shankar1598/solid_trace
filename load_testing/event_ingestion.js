/**
 * K6 Load Test: Event Ingestion Throughput
 *
 * Measures maximum event ingestion throughput using ramping-vus executor
 * to gradually increase load and identify the saturation point.
 *
 * Scenarios (controlled via SCENARIO env var):
 *   - single_issue: All events grouped into one issue (same fingerprint)
 *   - per_1000:     New issue created every 1000 events
 *   - per_100:      New issue created every 100 events
 *   - per_event:    Every event creates a new issue (unique fingerprints)
 *
 * Usage:
 *   k6 run -e SCENARIO=single_issue test/load/k6/event_ingestion.js
 *   k6 run -e SCENARIO=per_event -e MAX_VUS=200 test/load/k6/event_ingestion.js
 */

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { generateEvent, resetCounter } from './lib/payloads.js';
import {
  SCENARIO,
  MAX_VUS,
  DURATION,
  RAMP_UP,
  RAMP_DOWN,
  FINGERPRINT_INTERVALS,
  validateScenario,
  getStoreEndpoint,
  getAuthHeaders,
} from './lib/config.js';

// Validate scenario before starting
validateScenario(SCENARIO);

// Custom metrics
const eventsIngested = new Counter('events_ingested');
const eventsFailed = new Counter('events_failed');
const successRate = new Rate('success_rate');
const payloadSize = new Trend('payload_size_bytes');

// Export options for k6
export const options = {
  scenarios: {
    event_ingestion: {
      executor: 'ramping-vus',
      startVUs: 1,
      stages: [
        // Ramp up gradually to find saturation point
        { duration: RAMP_UP, target: Math.floor(MAX_VUS * 0.25) },  // 25% of max
        { duration: RAMP_UP, target: Math.floor(MAX_VUS * 0.5) },   // 50% of max
        { duration: RAMP_UP, target: Math.floor(MAX_VUS * 0.75) },  // 75% of max
        { duration: RAMP_UP, target: MAX_VUS },                      // 100% of max
        // Sustain at max VUs
        { duration: DURATION, target: MAX_VUS },
        // Ramp down
        { duration: RAMP_DOWN, target: 0 },
      ],
      gracefulRampDown: '10s',
    },
  },

  // Thresholds to track (not fail)
  thresholds: {
    http_req_duration: [
      { threshold: 'p(50)<100', abortOnFail: false },   // 50% under 100ms
      { threshold: 'p(95)<500', abortOnFail: false },   // 95% under 500ms
      { threshold: 'p(99)<1000', abortOnFail: false },  // 99% under 1s
    ],
    success_rate: [
      { threshold: 'rate>0.95', abortOnFail: false },   // 95% success rate
    ],
  },

  // Summary output configuration
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
};

// Setup function - runs once before the test
export function setup() {
  console.log(`
╔══════════════════════════════════════════════════════════════════╗
║           SolidTrace Event Ingestion Load Test                   ║
╠══════════════════════════════════════════════════════════════════╣
║  Scenario:     ${SCENARIO.padEnd(48)}                            ║
║  Max VUs:      ${String(MAX_VUS).padEnd(48)}                     ║
║  Duration:     ${DURATION.padEnd(48)}                            ║
║  Ramp Up:      ${RAMP_UP.padEnd(48)}                             ║
║  Endpoint:     ${getStoreEndpoint().padEnd(48)}                  ║
╚══════════════════════════════════════════════════════════════════╝
  `);

  // Reset the event counter
  resetCounter();

  // Verify the endpoint is reachable
  const healthCheck = http.get(getStoreEndpoint().replace('/store', '/health'), {
    timeout: '5s',
  });

  if (healthCheck.status !== 200) {
    console.warn(`⚠️  Health check failed (status ${healthCheck.status}). Proceeding anyway...`);
  }

  return {
    scenario: SCENARIO,
    fingerprintInterval: FINGERPRINT_INTERVALS[SCENARIO],
    endpoint: getStoreEndpoint(),
    headers: getAuthHeaders(),
  };
}

// Main test function - runs for each VU iteration
export default function (data) {
  // Generate event payload with appropriate fingerprint strategy
  const event = generateEvent(data.fingerprintInterval);
  const payload = JSON.stringify(event);

  // Track payload size
  payloadSize.add(payload.length);

  // Send event to the event store
  const response = http.post(data.endpoint, payload, {
    headers: data.headers,
    timeout: '10s',
  });

  // Check response and update metrics
  const isSuccess = check(response, {
    'status is 200': (r) => r.status === 200,
    'status is not 503 (backpressure)': (r) => r.status !== 503,
    'status is not 401 (auth)': (r) => r.status !== 401,
    'has response body': (r) => r.body && r.body.length > 0,
  });

  if (isSuccess) {
    eventsIngested.add(1);
    successRate.add(1);
  } else {
    eventsFailed.add(1);
    successRate.add(0);

    // Log first few failures for debugging
    if (__ITER < 10) {
      console.warn(`Request failed: status=${response.status}, body=${response.body}`);
    }
  }

  // Small sleep to prevent overwhelming the client
  // Remove or reduce this to maximize throughput
  sleep(0.001); // 1ms
}

// Teardown function - runs once after the test
export function teardown(data) {
  console.log(`
╔══════════════════════════════════════════════════════════════════╗
║                    Test Complete                                 ║
╠══════════════════════════════════════════════════════════════════╣
║  Review the metrics above to determine:                          ║
║  - Max RPS achieved (http_reqs rate)                             ║
║  - Latency at load (http_req_duration p95/p99)                   ║
║  - Error rate (success_rate)                                     ║
║  - Payload sizes (payload_size_bytes)                            ║
╚══════════════════════════════════════════════════════════════════╝
  `);
}

// Handle summary output
export function handleSummary(data) {
  const scenario = SCENARIO;
  const maxVUs = MAX_VUS;

  // Extract key metrics
  const rps = data.metrics.http_reqs?.values?.rate?.toFixed(2) || 'N/A';
  const p95 = data.metrics.http_req_duration?.values['p(95)']?.toFixed(2) || 'N/A';
  const p99 = data.metrics.http_req_duration?.values['p(99)']?.toFixed(2) || 'N/A';
  const successRateVal = (data.metrics.success_rate?.values?.rate * 100)?.toFixed(2) || 'N/A';
  const totalEvents = data.metrics.events_ingested?.values?.count || 0;
  const failedEvents = data.metrics.events_failed?.values?.count || 0;
  const avgPayloadSize = data.metrics.payload_size_bytes?.values?.avg?.toFixed(0) || 'N/A';

  console.log(`
╔══════════════════════════════════════════════════════════════════╗
║                    BENCHMARK RESULTS                             ║
╠══════════════════════════════════════════════════════════════════╣
║  Scenario:           ${scenario.padEnd(43)}║
║  Max VUs:            ${String(maxVUs).padEnd(43)}║
║  ────────────────────────────────────────────────────────────    ║
║  Max RPS:            ${String(rps + ' req/s').padEnd(43)}║
║  P95 Latency:        ${String(p95 + ' ms').padEnd(43)}║
║  P99 Latency:        ${String(p99 + ' ms').padEnd(43)}║
║  Success Rate:       ${String(successRateVal + '%').padEnd(43)}║
║  ────────────────────────────────────────────────────────────    ║
║  Total Events:       ${String(totalEvents).padEnd(43)}║
║  Failed Events:      ${String(failedEvents).padEnd(43)}║
║  Avg Payload Size:   ${String(avgPayloadSize + ' bytes').padEnd(43)}║
╚══════════════════════════════════════════════════════════════════╝

Copy this for README.md:
| ${scenario.padEnd(14)} | ${String(maxVUs).padEnd(6)} | ${String(rps).padEnd(10)} | ${String(p95 + 'ms').padEnd(10)} | ${String(p99 + 'ms').padEnd(10)} | ${String(successRateVal + '%').padEnd(10)} |
  `);

  return {
    stdout: JSON.stringify(data, null, 2),
  };
}
