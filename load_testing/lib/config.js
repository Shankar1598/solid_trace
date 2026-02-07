/**
 * K6 Load Test Configuration
 *
 * Shared configuration for all load test scripts.
 * Override values via environment variables when running k6.
 */

// Service URLs
export const EVENT_STORE_URL = __ENV.EVENT_STORE_URL || 'http://localhost:4000';
export const RAILS_URL = __ENV.RAILS_URL || 'http://localhost:3000';

// Project configuration (from db/seeds.rb)
export const PROJECT_ID = __ENV.PROJECT_ID || '1';
export const PUBLIC_KEY = __ENV.PUBLIC_KEY || 'testkey123';

// Test configuration
export const SCENARIO = __ENV.SCENARIO || 'single_issue';
export const MAX_VUS = parseInt(__ENV.MAX_VUS || '100', 10);
export const DURATION = __ENV.DURATION || '2m';
export const RAMP_UP = __ENV.RAMP_UP || '30s';
export const RAMP_DOWN = __ENV.RAMP_DOWN || '10s';

// Fingerprint rotation intervals for each scenario
export const FINGERPRINT_INTERVALS = {
  single_issue: -1,        // Never rotate - all events go to one issue
  per_1000: 1000,          // New issue every 1000 events
  per_100: 100,            // New issue every 100 events
  per_event: 1,            // New issue for every event
};

// Validate scenario
export function validateScenario(scenario) {
  const validScenarios = Object.keys(FINGERPRINT_INTERVALS);
  if (!validScenarios.includes(scenario)) {
    throw new Error(
      `Invalid scenario: ${scenario}. Valid options: ${validScenarios.join(', ')}`
    );
  }
}

// Build the event store endpoint URL
export function getStoreEndpoint() {
  return `${EVENT_STORE_URL}/api/${PROJECT_ID}/store`;
}

// Build auth headers
export function getAuthHeaders() {
  return {
    'Content-Type': 'application/json',
    'X-Sentry-Auth': `Sentry sentry_key=${PUBLIC_KEY}`,
  };
}
