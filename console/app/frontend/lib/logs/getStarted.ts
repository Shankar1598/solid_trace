// An empty Project's first steps: copy-paste snippets that send its Logs,
// with its ingest URL and key filled in, and the wait for a test Log to be
// readable.

/** Where a Project's Logs go: ingest as SDKs and shippers reach it, and the Project's key. */
export interface IngestTarget {
  url: string
  projectId: number
  key: string | null
}

export interface Snippet {
  id: 'curl' | 'vector' | 'fluent-bit' | 'sentry'
  label: string
  /** What the snippet is: a shell command or a config file. */
  note: string
  code: string
}

const NO_KEY = 'YOUR_PROJECT_KEY'

/** The JSON Log route, for curl and shippers. It finds the Project from the key alone. */
// Project#logs_url in app/models/project.rb builds the same path.
function logsRoute({ url }: IngestTarget): URL {
  const base = new URL(url)
  base.pathname = `${base.pathname.replace(/\/+$/, '')}/api/logs`
  return base
}

/** The DSN a Sentry SDK takes: the key as user, then ingest and the Project's id. */
export function sentryDsn(target: IngestTarget): string {
  const base = new URL(target.url)
  return `${base.protocol}//${target.key ?? NO_KEY}@${base.host}${base.pathname.replace(/\/+$/, '')}/${target.projectId}`
}

/** The snippets an empty Project shows: curl, Vector, Fluent Bit and a Sentry SDK. */
export function snippets(target: IngestTarget): Snippet[] {
  const key = target.key ?? NO_KEY
  const route = logsRoute(target)
  const https = route.protocol === 'https:'
  const port = route.port || (https ? '443' : '80')
  return [
    {
      id: 'curl',
      label: 'curl',
      note: 'Sends one Log. A JSON array or NDJSON sends many.',
      code: [
        `curl -X POST '${route}' \\`,
        `  -H 'Authorization: Bearer ${key}' \\`,
        `  -H 'Content-Type: application/json' \\`,
        `  -d '{"message": "Hello from curl", "level": "info", "service": "api"}'`,
      ].join('\n'),
    },
    {
      id: 'vector',
      label: 'Vector',
      note: "A sink for vector.yaml. Set inputs to your sources' names.",
      code: [
        'sinks:',
        '  solidtrace:',
        '    type: http',
        '    inputs: [app_logs]',
        `    uri: ${route}`,
        '    encoding:',
        '      codec: json',
        '    auth:',
        '      strategy: bearer',
        `      token: ${key}`,
      ].join('\n'),
    },
    {
      id: 'fluent-bit',
      label: 'Fluent Bit',
      note: 'An output for fluent-bit.conf.',
      code: [
        '[OUTPUT]',
        '    Name             http',
        '    Match            *',
        `    Host             ${route.hostname}`,
        `    Port             ${port}`,
        `    URI              ${route.pathname}`,
        ...(https ? ['    tls              On'] : []),
        '    Format           json',
        '    Json_date_key    time',
        '    Json_date_format iso8601',
        `    Header           Authorization Bearer ${key}`,
      ].join('\n'),
    },
    {
      id: 'sentry',
      label: 'Sentry SDK',
      note: 'JavaScript (@sentry/node). Other Sentry SDKs take the same DSN.',
      code: [
        "import * as Sentry from '@sentry/node'",
        '',
        'Sentry.init({',
        `  dsn: '${sentryDsn(target)}',`,
        '  enableLogs: true,',
        '})',
        '',
        "Sentry.logger.info('Hello from Sentry', { service: 'api' })",
      ].join('\n'),
    },
  ]
}

const sleepFor = (ms: number) => new Promise<void>(resolve => setTimeout(resolve, ms))

/**
 * Checks up to `tries` times, `delayMs` apart, until `check` answers true:
 * a Log ingest has stored is readable a moment later, once Log processing
 * has it. False when it never was.
 */
export async function waitFor(
  check: () => Promise<boolean>,
  { tries = 20, delayMs = 500, sleep = sleepFor }: { tries?: number; delayMs?: number; sleep?: (ms: number) => Promise<void> } = {},
): Promise<boolean> {
  for (let i = 0; i < tries; i++) {
    if (i > 0) await sleep(delayMs)
    if (await check()) return true
  }
  return false
}
