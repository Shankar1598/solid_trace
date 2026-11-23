import { describe, expect, it } from 'vitest'
import { sentryDsn, snippets, waitFor, type IngestTarget } from './getStarted'

const ingest: IngestTarget = { url: 'http://localhost:4000', projectId: 7, key: 'abc123' }
const code = (id: string, to = ingest) => snippets(to).find(s => s.id === id)!.code

describe('snippets for an empty Project', () => {
  it('offers curl, Vector, Fluent Bit, a Sentry SDK, an OpenTelemetry Collector and an OpenTelemetry SDK, in that order', () => {
    expect(snippets(ingest).map(s => s.label)).toEqual(['curl', 'Vector', 'Fluent Bit', 'Sentry SDK', 'OpenTelemetry Collector', 'OpenTelemetry SDK'])
    expect(snippets(ingest).map(s => s.id).slice(4)).toEqual(['otel-collector', 'otel-sdk'])
  })

  it('fills in the key-only Log route and the Project\'s key', () => {
    expect(code('curl')).toContain("'http://localhost:4000/api/logs'")
    expect(code('curl')).toContain("-H 'Authorization: Bearer abc123'")
    expect(code('vector')).toContain('uri: http://localhost:4000/api/logs')
    expect(code('vector')).toContain('token: abc123')
  })

  it('splits the URL for Fluent Bit, which reads time from the Log\'s time key', () => {
    const fluent = code('fluent-bit')
    expect(fluent).toMatch(/Host +localhost\n/)
    expect(fluent).toMatch(/Port +4000\n/)
    expect(fluent).toMatch(/URI +\/api\/logs\n/)
    expect(fluent).toMatch(/Header +Authorization Bearer abc123/)
    expect(fluent).toMatch(/Json_date_key +time\n/)
    expect(fluent).not.toContain('tls')

    const secure = code('fluent-bit', { ...ingest, url: 'https://logs.example.com/ingest/' })
    expect(secure).toMatch(/Host +logs.example.com\n/)
    expect(secure).toMatch(/Port +443\n/)
    expect(secure).toMatch(/URI +\/ingest\/api\/logs\n/)
    expect(secure).toMatch(/tls +On\n/)
  })

  it('gives the Sentry SDK a DSN of the key, ingest and Project', () => {
    expect(sentryDsn(ingest)).toBe('http://abc123@localhost:4000/7')
    expect(sentryDsn({ ...ingest, url: 'https://logs.example.com/ingest/' })).toBe('https://abc123@logs.example.com/ingest/7')
    expect(code('sentry')).toContain("dsn: 'http://abc123@localhost:4000/7'")
  })

  it('gives the Collector a config file with receivers, the bare Ingest URL and the key', () => {
    const config = code('otel-collector')
    // No YAML parser is in the dependencies, so the shape is checked by lines.
    expect(config.split('\n').filter(l => /^\S/.test(l))).toEqual(['receivers:', 'exporters:', 'service:'])
    expect(config).toContain('receivers:\n  otlp:\n    protocols:\n      grpc: {}\n      http: {}\n')
    expect(config).toContain('exporters:\n  otlp_http:\n')
    expect(config).not.toContain('otlphttp')
    expect(config).toMatch(/\n    endpoint: http:\/\/localhost:4000\n/)
    expect(config).toContain('    headers:\n      Authorization: "Bearer abc123"')
    expect(config).toContain('  pipelines:\n    logs:\n      receivers: [otlp]\n      exporters: [otlp_http]')
    expect(config).not.toContain('OTEL_')
    expect(config).not.toContain('/api/')
    expect(config).not.toContain('/v1/logs')

    const secure = code('otel-collector', { ...ingest, url: 'https://logs.example.com/ingest/' })
    expect(secure).toContain('    endpoint: https://logs.example.com/ingest\n')
  })

  it('gives the SDK only environment variables, with the header percent-encoded', () => {
    const sdk = code('otel-sdk')
    expect(sdk.split('\n')).toEqual([
      'OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://localhost:4000/v1/logs',
      'OTEL_EXPORTER_OTLP_LOGS_HEADERS=Authorization=Bearer%20abc123',
      'OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf',
    ])
    expect(sdk).not.toContain('/api/')
    expect(code('otel-sdk', { ...ingest, url: 'https://logs.example.com/ingest/' })).toContain(
      'OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=https://logs.example.com/ingest/v1/logs\n',
    )
  })

  it('leaves a placeholder for a Project without a key', () => {
    const noKey = { ...ingest, key: null }
    expect(code('curl', noKey)).toContain('Bearer YOUR_PROJECT_KEY')
    expect(code('otel-collector', noKey)).toContain('Authorization: "Bearer YOUR_PROJECT_KEY"')
    expect(code('otel-sdk', noKey)).toContain('Authorization=Bearer%20YOUR_PROJECT_KEY')
  })
})

describe('waiting for a test Log', () => {
  const noSleep = { sleep: async () => {} }

  it('checks until the Log can be read, pausing between checks', async () => {
    const answers = [false, false, true]
    const pauses: number[] = []
    const found = await waitFor(async () => answers.shift()!, { tries: 5, delayMs: 300, sleep: async ms => void pauses.push(ms) })
    expect(found).toBe(true)
    expect(pauses).toEqual([300, 300])
  })

  it('gives up after its tries', async () => {
    let checks = 0
    expect(await waitFor(async () => (checks++, false), { tries: 4, ...noSleep })).toBe(false)
    expect(checks).toBe(4)
  })
})
