import { describe, expect, it } from 'vitest'
import { sentryDsn, snippets, waitFor, type IngestTarget } from './getStarted'

const ingest: IngestTarget = { url: 'http://localhost:4000', projectId: 7, key: 'abc123' }
const code = (id: string, to = ingest) => snippets(to).find(s => s.id === id)!.code

describe('snippets for an empty Project', () => {
  it('offers curl, Vector, Fluent Bit and a Sentry SDK, in that order', () => {
    expect(snippets(ingest).map(s => s.label)).toEqual(['curl', 'Vector', 'Fluent Bit', 'Sentry SDK'])
  })

  it('fills in the Project\'s Log route and key', () => {
    expect(code('curl')).toContain("'http://localhost:4000/api/7/logs'")
    expect(code('curl')).toContain("-H 'Authorization: Bearer abc123'")
    expect(code('vector')).toContain('uri: http://localhost:4000/api/7/logs')
    expect(code('vector')).toContain('token: abc123')
  })

  it('splits the URL for Fluent Bit, which reads time from the Log\'s time key', () => {
    const fluent = code('fluent-bit')
    expect(fluent).toMatch(/Host +localhost\n/)
    expect(fluent).toMatch(/Port +4000\n/)
    expect(fluent).toMatch(/URI +\/api\/7\/logs\n/)
    expect(fluent).toMatch(/Header +Authorization Bearer abc123/)
    expect(fluent).toMatch(/Json_date_key +time\n/)
    expect(fluent).not.toContain('tls')

    const secure = code('fluent-bit', { ...ingest, url: 'https://logs.example.com/ingest/' })
    expect(secure).toMatch(/Host +logs.example.com\n/)
    expect(secure).toMatch(/Port +443\n/)
    expect(secure).toMatch(/URI +\/ingest\/api\/7\/logs\n/)
    expect(secure).toMatch(/tls +On\n/)
  })

  it('gives the Sentry SDK a DSN of the key, ingest and Project', () => {
    expect(sentryDsn(ingest)).toBe('http://abc123@localhost:4000/7')
    expect(sentryDsn({ ...ingest, url: 'https://logs.example.com/ingest/' })).toBe('https://abc123@logs.example.com/ingest/7')
    expect(code('sentry')).toContain("dsn: 'http://abc123@localhost:4000/7'")
  })

  it('leaves a placeholder for a Project without a key', () => {
    expect(code('curl', { ...ingest, key: null })).toContain('Bearer YOUR_PROJECT_KEY')
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
