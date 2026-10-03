import { describe, expect, it } from 'vitest'
import { inspectorFields, inspectorJson, utcTime } from './inspector'
import type { Log } from './stream'

const log: Log = {
  id: '0199a8b2-6a1e-7c3d-9f00-1234567890ab',
  time: '2026-10-03T09:15:00.123456789Z',
  level: 'error',
  message: 'payment failed',
  service: 'api',
  trace_id: 'abc',
  attributes: { 'http.status': 502, cached: false, 'user.id': 'u-1', tags: ['a', 'b'] },
}

describe('the inspector', () => {
  it('lists the built-in fields a Log has, then its Attributes by name, each with its own type', () => {
    expect(inspectorFields(log)).toEqual([
      { name: 'level', value: 'error' },
      { name: 'service', value: 'api' },
      { name: 'message', value: 'payment failed' },
      { name: 'trace_id', value: 'abc' },
      { name: 'cached', value: false },
      { name: 'http.status', value: 502 },
      { name: 'tags', value: ['a', 'b'] },
      { name: 'user.id', value: 'u-1' },
    ])
  })

  it('shows the raw time in UTC, to the nanosecond EventStore gave', () => {
    expect(utcTime('2026-10-03T09:15:00.123456789Z')).toBe('2026-10-03T09:15:00.123456789Z')
  })

  it('turns a time with an offset into UTC', () => {
    expect(utcTime('2026-10-03T11:15:00.5+02:00')).toBe('2026-10-03T09:15:00.500Z')
  })

  it('writes the Log as JSON with its raw UTC time', () => {
    const json = JSON.parse(inspectorJson({ ...log, time: '2026-10-03T11:15:00+02:00' }))

    expect(json.time).toBe('2026-10-03T09:15:00.000Z')
    expect(json.attributes['http.status']).toBe(502)
    expect(json.id).toBe(log.id)
  })
})
