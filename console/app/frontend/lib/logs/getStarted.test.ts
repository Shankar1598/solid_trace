import { describe, expect, it } from 'vitest'
import { sentryDsn, snippets, waitFor, type IngestTarget } from './getStarted'

const ingest: IngestTarget = { url: 'http://localhost:4000', projectId: 7, key: 'abc123' }
const code = (id: string, to = ingest) => snippets(to).find(s => s.id === id)!.code

describe('snippets for an empty Project', () => {
  it('offers curl, Vector, Fluent Bit, a Sentry SDK and OpenTelemetry, in that order', () => {
    expect(snippets(ingest).map(s => s.label)).toEqual(['curl', 'Vector', 'Fluent Bit', 'Sentry SDK', 'OpenTelemetry'])
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

  it('gives OpenTelemetry the bare Ingest URL and key, for a Collector and for an SDK', () => {
    const otel = code('opentelemetry')
    expect(otel).toContain('otlp_http:')
    expect(otel).not.toContain('otlphttp')
    expect(otel).toMatch(/\n    endpoint: http:\/\/localhost:4000\n/)
    expect(otel).toContain('      Authorization: "Bearer abc123"')
    expect(otel).toContain('exporters: [otlp_http]')
    expect(otel).toContain('OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://localhost:4000/v1/logs\n')
    expect(otel).toContain('OTEL_EXPORTER_OTLP_LOGS_HEADERS=Authorization=Bearer abc123\n')
    expect(otel).toContain('OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf')
    expect(otel).not.toContain('/api/')

    const secure = code('opentelemetry', { ...ingest, url: 'https://logs.example.com/ingest/' })
    expect(secure).toContain('    endpoint: https://logs.example.com/ingest\n')
    expect(secure).toContain('OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=https://logs.example.com/ingest/v1/logs\n')
    expect(secure).not.toContain('/api/')
  })

  it('leaves a placeholder for a Project without a key', () => {
    expect(code('curl', { ...ingest, key: null })).toContain('Bearer YOUR_PROJECT_KEY')
    expect(code('opentelemetry', { ...ingest, key: null })).toContain('Authorization: "Bearer YOUR_PROJECT_KEY"')
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
