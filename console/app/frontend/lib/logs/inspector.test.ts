import { describe, expect, it } from 'vitest'
import { aroundLog, aroundMissed, inspectorFields, inspectorJson, jsonLines, localTime, narrowAround, utcTime, type JsonLine } from './inspector'
import { MAX_STREAM_LOGS, PAGE_LOGS, type Log, type Stream } from './stream'

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

describe('the inspector\'s JSON lines', () => {
  const lines = jsonLines({
    id: 'x',
    time: '2026-10-03T09:15:00Z',
    level: 'error',
    message: 'boom',
    attributes: { 'http.status': 502, tags: ['a', { deep: 1 }], empty: [], cached: null },
  })
  const text = (l: JsonLine) => `${'  '.repeat(l.depth)}${l.key !== undefined ? `"${l.key}": ` : ''}${l.text}${l.comma ? ',' : ''}`

  it('writes the Log as inspectorJson does, a line each', () => {
    expect(lines.map(text).join('\n')).toBe(JSON.stringify({
      id: 'x', time: '2026-10-03T09:15:00Z', level: 'error', message: 'boom',
      attributes: { 'http.status': 502, tags: ['a', { deep: 1 }], empty: [], cached: null },
    }, null, 2))
  })

  it('marks the Fields: the Log\'s own but id, and its Attributes; an array\'s elements are its', () => {
    const fieldOf = (key: string) => lines.find(l => l.key === key)?.field
    expect(fieldOf('id')).toBeUndefined()
    expect(fieldOf('time')).toBe('time')
    expect(fieldOf('attributes')).toBeUndefined()
    expect(fieldOf('http.status')).toBe('http.status')
    expect(fieldOf('tags')).toBe('tags')
    expect(lines.find(l => l.text === '"a"')?.elementOf).toBe('tags')
    expect(fieldOf('deep')).toBeUndefined()
    expect(lines.find(l => l.key === 'empty')).toMatchObject({ field: 'empty', kind: 'empty', value: [] })
  })

  it('knows where each bracket closes, and how much it holds', () => {
    const tags = lines.findIndex(l => l.key === 'tags')
    expect(lines[tags]).toMatchObject({ kind: 'open', size: 2, value: ['a', { deep: 1 }] })
    expect(lines[lines[tags].end!]).toMatchObject({ kind: 'close', text: ']', comma: true })
    expect(lines[0].end).toBe(lines.length - 1)
  })
})

describe('a Log\'s local time', () => {
  it('reads as a date and a clock to the millisecond, with the zone', () => {
    expect(localTime('2026-10-03T09:15:00.123Z', 'UTC')).toBe('Oct 3, 2026 at 09:15:00.123 UTC')
    expect(localTime('2026-10-03T09:15:00.123Z', 'Europe/London')).toBe('Oct 3, 2026 at 10:15:00.123 GMT+1')
  })
})

describe('Logs around this one', () => {
  const log: Log = { id: 'l1', time: '2026-10-17T12:00:00.250Z', message: 'boom', service: 'api', attributes: {} }

  it('reads the same Service from a minute before the Log to a minute after', () => {
    const { builder, range } = aroundLog(log)
    expect(builder).toEqual({ filters: [{ field: 'service', op: '==', value: 'api' }], search: '' })
    expect(range).toEqual({ kind: 'absolute', from: new Date('2026-10-17T11:59:00.250Z'), to: new Date('2026-10-17T12:01:00.250Z') })
  })

  it('reads Logs without a Service around a Log without one', () => {
    expect(aroundLog({ ...log, service: undefined }).builder.filters).toEqual([{ field: 'service', op: 'missing', value: null }])
  })

  describe('when more than a page is newer than the Log', () => {
    const range = aroundLog(log).range
    const at = (ms: number): Log => ({ id: `n${ms}`, time: new Date(ms).toISOString(), message: 'tick', service: 'api', attributes: {} })
    /** A stream of `count` Logs, one every `stepMs` back from the range's end. */
    const streamOf = (count: number, stepMs: number, nextCursor: string | null = 'more'): Stream => ({
      logs: Array.from({ length: count }, (_, i) => at(range.to.getTime() - stepMs * (i + 1))),
      nextCursor,
      target: PAGE_LOGS,
    })

    it('ends the range as long after the Log as the Logs loaded took to fill half a page', () => {
      // 200 Logs 10 ms apart took 2 s, so 100 fit in the first second after the Log.
      expect(narrowAround(range, streamOf(200, 10), log)).toEqual({
        kind: 'absolute', from: range.from, to: new Date('2026-10-17T12:00:01.250Z'),
      })
    })

    it('ends it just after the Log when the Logs come faster than a millisecond apart', () => {
      expect(narrowAround(range, streamOf(200, 0.001), log)?.to).toEqual(new Date('2026-10-17T12:00:00.251Z'))
    })

    it('leaves the range alone when narrowing it would end it no sooner, so older pages load instead', () => {
      // 10 Logs over 30 s: half a page would need longer than the minute after the Log.
      expect(narrowAround(range, streamOf(10, 3_000), log)).toBeNull()
      // Already ending just after the Log.
      const tight = { ...range, to: new Date('2026-10-17T12:00:00.251Z') }
      expect(narrowAround(tight, { logs: [at(tight.to.getTime())], nextCursor: 'more', target: PAGE_LOGS }, log)).toBeNull()
      expect(narrowAround(range, streamOf(0, 10), log)).toBeNull()
    })

    it('says why a stream that ended without the Log does not show it', () => {
      expect(aroundMissed(streamOf(MAX_STREAM_LOGS, 1))).toBe(
        `This Log is more than ${MAX_STREAM_LOGS.toLocaleString('en-US')} Logs into the stream, so it can't be shown. Pick a shorter range around it.`)
      expect(aroundMissed(streamOf(20, 10, null))).toBe("This Log isn't in the stream any more: it may have left the Project's Retention.")
    })
  })
})
