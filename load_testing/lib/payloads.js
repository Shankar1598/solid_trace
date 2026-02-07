/**
 * K6 Load Test Payload Generator
 *
 * Generates large, realistic Sentry-compatible event payloads (~8-10KB each)
 * with detailed stacktraces, breadcrumbs, contexts, and metadata.
 */

// Simple random hex string generator (k6-compatible)
function randomHex(length) {
  const chars = '0123456789abcdef';
  let result = '';
  for (let i = 0; i < length; i++) {
    result += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return result;
}

// Generate a random integer between min and max (inclusive)
function randomInt(min, max) {
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

// Generate a UUID v4 format string
function generateUUID() {
  return `${randomHex(8)}-${randomHex(4)}-4${randomHex(3)}-${randomHex(4)}-${randomHex(12)}`;
}

// Realistic file paths for stacktrace frames
const FILE_PATHS = [
  'app/controllers/api/events_controller.rb',
  'app/controllers/api/v1/issues_controller.rb',
  'app/controllers/application_controller.rb',
  'app/models/event.rb',
  'app/models/issue.rb',
  'app/models/project.rb',
  'app/models/organization.rb',
  'app/services/event_processor.rb',
  'app/services/issue_aggregator.rb',
  'app/services/fingerprint_calculator.rb',
  'app/jobs/process_event_job.rb',
  'app/jobs/notify_slack_job.rb',
  'lib/sentry/transport.rb',
  'lib/sentry/envelope.rb',
  'lib/event_store/client.rb',
  'lib/event_store/connection.rb',
  'lib/utils/compression.rb',
  'lib/utils/serialization.rb',
  'config/initializers/sentry.rb',
  'config/initializers/event_store.rb',
  'vendor/bundle/ruby/3.2.0/gems/rails-7.1.0/lib/action_controller/base.rb',
  'vendor/bundle/ruby/3.2.0/gems/activesupport-7.1.0/lib/active_support/callbacks.rb',
  'vendor/bundle/ruby/3.2.0/gems/rack-3.0.0/lib/rack/handler.rb',
  'vendor/bundle/ruby/3.2.0/gems/puma-6.4.0/lib/puma/server.rb',
  '/usr/local/lib/ruby/3.2.0/net/http.rb',
  '/usr/local/lib/ruby/3.2.0/timeout.rb',
  '/usr/local/lib/ruby/3.2.0/socket.rb',
];

// Function names for stacktrace frames
const FUNCTION_NAMES = [
  'call', 'process', 'handle', 'execute', 'run', 'perform', 'invoke',
  'dispatch', 'route', 'render', 'validate', 'authenticate', 'authorize',
  'serialize', 'deserialize', 'compress', 'decompress', 'encrypt', 'decrypt',
  'connect', 'disconnect', 'read', 'write', 'send', 'receive', 'fetch',
  'store', 'retrieve', 'index', 'show', 'create', 'update', 'destroy',
  'before_action', 'after_action', 'around_action', 'rescue_from',
];

// Error types for exceptions
const ERROR_TYPES = [
  'ZeroDivisionError',
  'NoMethodError',
  'ArgumentError',
  'RuntimeError',
  'TypeError',
  'NameError',
  'IOError',
  'Timeout::Error',
  'ActiveRecord::RecordNotFound',
  'ActiveRecord::RecordInvalid',
  'ActionController::ParameterMissing',
  'Net::ReadTimeout',
  'Redis::ConnectionError',
  'Errno::ECONNREFUSED',
];

// Error messages
const ERROR_MESSAGES = [
  'divided by 0',
  "undefined method `foo' for nil:NilClass",
  'wrong number of arguments (given 2, expected 1)',
  'unexpected nil value',
  'invalid type: expected String, got Integer',
  "uninitialized constant MyClass",
  'stream closed in another thread',
  'execution expired',
  "Couldn't find Issue with 'id'=999",
  "Validation failed: Name can't be blank",
  'param is missing or the value is empty: event',
  'Net::ReadTimeout with #<TCPSocket:(closed)>',
  'Error connecting to Redis on localhost:6379',
  'Connection refused - connect(2) for 127.0.0.1:5432',
];

// Breadcrumb categories
const BREADCRUMB_CATEGORIES = [
  'http', 'navigation', 'ui.click', 'console', 'xhr', 'fetch',
  'websocket', 'query', 'redis', 'sidekiq', 'mailer',
];

// Browser names for context
const BROWSERS = ['Chrome', 'Firefox', 'Safari', 'Edge', 'Opera'];
const OS_NAMES = ['macOS', 'Windows', 'Linux', 'iOS', 'Android'];
const DEVICE_TYPES = ['Desktop', 'Mobile', 'Tablet'];

/**
 * Generate a realistic stacktrace with 20-30 frames
 */
function generateStacktrace() {
  const frameCount = randomInt(20, 30);
  const frames = [];

  for (let i = 0; i < frameCount; i++) {
    const filePath = FILE_PATHS[randomInt(0, FILE_PATHS.length - 1)];
    const functionName = FUNCTION_NAMES[randomInt(0, FUNCTION_NAMES.length - 1)];
    const isAppCode = !filePath.includes('vendor/') && !filePath.startsWith('/usr/');

    const frame = {
      filename: filePath,
      abs_path: `/app/${filePath}`,
      function: functionName,
      lineno: randomInt(1, 500),
      colno: randomInt(1, 120),
      in_app: isAppCode,
      context_line: `    result = ${functionName}(params[:id], options)`,
      pre_context: [
        '  def process_request',
        '    validate_params!',
        '    @resource = find_resource',
      ],
      post_context: [
        '    render json: result',
        '  rescue StandardError => e',
        '    handle_error(e)',
      ],
      vars: {
        self: `#<${filePath.includes('controller') ? 'EventsController' : 'EventService'}:0x${randomHex(12)}>`,
        params: '{ id: 123, format: "json" }',
        options: '{ async: true, timeout: 30 }',
      },
    };

    frames.push(frame);
  }

  return { frames };
}

/**
 * Generate breadcrumbs array (10-15 entries)
 */
function generateBreadcrumbs() {
  const count = randomInt(10, 15);
  const breadcrumbs = [];
  let timestamp = Date.now() - count * 1000;

  for (let i = 0; i < count; i++) {
    const category = BREADCRUMB_CATEGORIES[randomInt(0, BREADCRUMB_CATEGORIES.length - 1)];

    const breadcrumb = {
      type: category === 'http' ? 'http' : 'default',
      category: category,
      level: i === count - 1 ? 'error' : 'info',
      message: `Breadcrumb ${i + 1}: ${category} action completed`,
      timestamp: new Date(timestamp).toISOString(),
      data: {
        url: `https://api.example.com/v1/resource/${randomInt(1, 1000)}`,
        method: ['GET', 'POST', 'PUT', 'DELETE'][randomInt(0, 3)],
        status_code: i === count - 1 ? 500 : 200,
        duration_ms: randomInt(10, 500),
      },
    };

    breadcrumbs.push(breadcrumb);
    timestamp += randomInt(500, 2000);
  }

  return breadcrumbs;
}

/**
 * Generate browser/OS/device contexts
 */
function generateContexts() {
  return {
    browser: {
      name: BROWSERS[randomInt(0, BROWSERS.length - 1)],
      version: `${randomInt(90, 120)}.0.${randomInt(1000, 9999)}.${randomInt(10, 99)}`,
    },
    os: {
      name: OS_NAMES[randomInt(0, OS_NAMES.length - 1)],
      version: `${randomInt(10, 14)}.${randomInt(0, 6)}.${randomInt(0, 3)}`,
    },
    device: {
      family: DEVICE_TYPES[randomInt(0, DEVICE_TYPES.length - 1)],
      model: `Model-${randomHex(4)}`,
      brand: ['Apple', 'Samsung', 'Google', 'Dell', 'HP'][randomInt(0, 4)],
    },
    runtime: {
      name: 'ruby',
      version: '3.2.2',
    },
    app: {
      app_name: 'SolidTrace',
      app_version: '1.0.0',
      app_build: randomHex(8),
    },
    trace: {
      trace_id: randomHex(32),
      span_id: randomHex(16),
      parent_span_id: randomHex(16),
      op: 'http.server',
      status: 'internal_error',
    },
  };
}

/**
 * Generate user context
 */
function generateUser() {
  const userId = randomInt(1, 10000);
  return {
    id: userId.toString(),
    email: `user${userId}@example.com`,
    username: `user_${randomHex(6)}`,
    ip_address: `${randomInt(1, 255)}.${randomInt(0, 255)}.${randomInt(0, 255)}.${randomInt(1, 254)}`,
    geo: {
      city: ['San Francisco', 'New York', 'London', 'Tokyo', 'Berlin'][randomInt(0, 4)],
      country_code: ['US', 'GB', 'JP', 'DE', 'FR'][randomInt(0, 4)],
      region: 'Region-' + randomHex(2),
    },
  };
}

/**
 * Generate extra data with various metadata
 */
function generateExtra() {
  return {
    request_id: generateUUID(),
    correlation_id: generateUUID(),
    session_id: randomHex(32),
    feature_flags: {
      new_dashboard: true,
      beta_api: false,
      dark_mode: true,
    },
    performance: {
      db_query_count: randomInt(5, 50),
      db_query_time_ms: randomInt(10, 500),
      cache_hits: randomInt(0, 20),
      cache_misses: randomInt(0, 10),
      memory_mb: randomInt(100, 500),
    },
    request: {
      url: `https://app.solidtrace.io/api/v1/projects/${randomInt(1, 100)}/events`,
      method: 'POST',
      headers: {
        'User-Agent': `Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36`,
        'Accept': 'application/json',
        'Content-Type': 'application/json',
      },
      body_size: randomInt(100, 10000),
    },
  };
}

/**
 * Global counter for fingerprint rotation
 */
let globalEventCounter = 0;
let currentFingerprint = randomHex(32);

/**
 * Generate or rotate fingerprint based on interval
 * @param {number} interval - How often to create a new fingerprint (-1 = never)
 */
export function getFingerprint(interval) {
  globalEventCounter++;

  if (interval === -1) {
    // Single issue scenario - always use same fingerprint
    return currentFingerprint;
  }

  if (interval === 1) {
    // Per-event scenario - always new fingerprint
    return randomHex(32);
  }

  // Rotate every N events
  if (globalEventCounter % interval === 1) {
    currentFingerprint = randomHex(32);
  }

  return currentFingerprint;
}

/**
 * Reset the global counter (useful between test runs)
 */
export function resetCounter() {
  globalEventCounter = 0;
  currentFingerprint = randomHex(32);
}

/**
 * Generate a large, realistic Sentry-compatible event payload
 * @param {number} fingerprintInterval - How often to rotate fingerprint
 * @returns {object} Event payload object
 */
export function generateEvent(fingerprintInterval) {
  const errorIndex = randomInt(0, ERROR_TYPES.length - 1);
  const fingerprint = getFingerprint(fingerprintInterval);

  return {
    event_id: randomHex(32),
    level: 'error',
    timestamp: new Date().toISOString(),
    platform: 'ruby',
    release: `solidtrace@1.0.0+${randomHex(8)}`,
    dist: 'production',
    environment: 'load-test',
    server_name: `web-${randomInt(1, 10)}.solidtrace.io`,
    transaction: `/api/v1/projects/${randomInt(1, 100)}/events`,
    logger: 'rails',

    // Custom fingerprint for issue grouping
    fingerprint: [fingerprint],

    // Tags for filtering
    tags: {
      environment: 'load-test',
      server: `web-${randomInt(1, 10)}`,
      region: ['us-east-1', 'us-west-2', 'eu-west-1', 'ap-northeast-1'][randomInt(0, 3)],
      version: '1.0.0',
      ruby_version: '3.2.2',
      rails_version: '7.1.0',
      handler: 'EventsController#create',
      k6_test: 'true',
    },

    // Main error information
    exception: {
      values: [
        {
          type: ERROR_TYPES[errorIndex],
          value: `${ERROR_MESSAGES[errorIndex]} (${ERROR_TYPES[errorIndex]})`,
          module: 'app.controllers.api',
          thread_id: randomInt(1, 100),
          mechanism: {
            type: 'generic',
            handled: false,
            synthetic: false,
          },
          stacktrace: generateStacktrace(),
        },
      ],
    },

    // Message (fallback)
    message: {
      formatted: `Error in EventsController: ${ERROR_MESSAGES[errorIndex]}`,
      message: ERROR_MESSAGES[errorIndex],
      params: ['event_id', randomInt(1, 10000).toString()],
    },

    // Culprit (deprecated but included for compatibility)
    culprit: 'app/controllers/api/events_controller.rb in create',

    // Detailed contexts
    contexts: generateContexts(),

    // User information
    user: generateUser(),

    // Breadcrumb trail
    breadcrumbs: {
      values: generateBreadcrumbs(),
    },

    // Extra metadata
    extra: generateExtra(),

    // Request data
    request: {
      url: `https://app.solidtrace.io/api/v1/projects/${randomInt(1, 100)}/events`,
      method: 'POST',
      headers: {
        'Host': 'app.solidtrace.io',
        'User-Agent': 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)',
        'Accept': 'application/json',
        'Accept-Language': 'en-US,en;q=0.9',
        'Accept-Encoding': 'gzip, deflate, br',
        'Content-Type': 'application/json',
        'Origin': 'https://app.solidtrace.io',
        'Referer': 'https://app.solidtrace.io/dashboard',
        'X-Request-ID': generateUUID(),
        'X-Forwarded-For': `${randomInt(1, 255)}.${randomInt(0, 255)}.${randomInt(0, 255)}.${randomInt(1, 254)}`,
        'X-Forwarded-Proto': 'https',
      },
      env: {
        REMOTE_ADDR: `${randomInt(1, 255)}.${randomInt(0, 255)}.${randomInt(0, 255)}.${randomInt(1, 254)}`,
        SERVER_NAME: 'app.solidtrace.io',
        SERVER_PORT: '443',
      },
      data: {
        event: {
          message: 'Sample event data',
          level: 'error',
          timestamp: new Date().toISOString(),
        },
      },
    },

    // SDK information
    sdk: {
      name: 'sentry.ruby',
      version: '5.16.0',
      integrations: ['rack', 'rails', 'sidekiq', 'redis'],
      packages: [
        { name: 'gem:sentry-ruby', version: '5.16.0' },
        { name: 'gem:sentry-rails', version: '5.16.0' },
        { name: 'gem:sentry-sidekiq', version: '5.16.0' },
      ],
    },

    // Modules/dependencies
    modules: {
      rails: '7.1.0',
      puma: '6.4.0',
      sidekiq: '7.2.0',
      redis: '5.0.8',
      pg: '1.5.4',
      'sentry-ruby': '5.16.0',
    },
  };
}
