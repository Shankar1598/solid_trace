import { describe, expect, it } from 'vitest'
import { EMPTY_BUILDER, queryOf } from './builder'
import { barAt, bucketSeconds, errorRate, filterByLevel, zoomRange, type LevelCounts } from './histogram'

const MINUTE = 60
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

function span(ms: number): [Date, Date] {
  const to = new Date('2026-10-03T10:00:00Z')
  return [new Date(to.getTime() - ms), to]
}

describe('the bucket size of a range', () => {
  it.each([
    ['15 minutes', 15 * 60_000, 10],
    ['an hour', 3_600_000, MINUTE],
    ['2 hours', 2 * 3_600_000, 2 * MINUTE],
    ['6 hours', 6 * 3_600_000, 5 * MINUTE],
    ['24 hours', 24 * 3_600_000, 15 * MINUTE],
    ['7 days', 7 * 86_400_000, 2 * HOUR],
    ['30 days', 30 * 86_400_000, 12 * HOUR],
  ])('over %s is %i seconds', (_, ms, seconds) => {
    expect(bucketSeconds(...span(ms))).toBe(seconds)
  })

  it('is a second for a range of a few seconds, or none at all', () => {
    expect(bucketSeconds(...span(5_000))).toBe(1)
    expect(bucketSeconds(...span(0))).toBe(1)
  })

  it('stops growing at a week, for ranges of years', () => {
    expect(bucketSeconds(...span(3 * 365 * 86_400_000))).toBe(7 * DAY)
  })
})

/** Empty one-minute buckets from `first` for `n` minutes, as EventStore answers them. */
function minutes(first: string, n: number) {
  const start = new Date(first).getTime()
  return {
    bucket_seconds: 60,
    buckets: Array.from({ length: n }, (_, i) => ({ time: new Date(start + i * 60_000).toISOString() })),
  }
}

const at = (iso: string) => new Date(iso)

describe('zooming from the strip', () => {
  const hour = { from: at('2026-10-03T09:00:00Z'), to: at('2026-10-03T10:00:00Z') }

  it('zooms to the bucket of a clicked bar', () => {
    expect(zoomRange(minutes('2026-10-03T09:00:00Z', 60), hour, 30, 30)).toEqual({
      kind: 'absolute', from: at('2026-10-03T09:30:00Z'), to: at('2026-10-03T09:31:00Z'),
    })
  })

  it('zooms across every bar a drag covers, in either direction', () => {
    const expected = { kind: 'absolute', from: at('2026-10-03T09:35:00Z'), to: at('2026-10-03T09:41:00Z') }

    expect(zoomRange(minutes('2026-10-03T09:00:00Z', 60), hour, 35, 40)).toEqual(expected)
    expect(zoomRange(minutes('2026-10-03T09:00:00Z', 60), hour, 40, 35)).toEqual(expected)
  })

  it('never zooms outside the range, though the end buckets reach past it', () => {
    const range = { from: at('2026-10-03T09:00:30Z'), to: at('2026-10-03T10:00:30Z') }
    const histogram = minutes('2026-10-03T09:00:00Z', 61)

    expect(zoomRange(histogram, range, 0, 0)).toEqual({
      kind: 'absolute', from: at('2026-10-03T09:00:30Z'), to: at('2026-10-03T09:01:00Z'),
    })
    expect(zoomRange(histogram, range, 60, 60)).toEqual({
      kind: 'absolute', from: at('2026-10-03T10:00:00Z'), to: at('2026-10-03T10:00:30Z'),
    })
    expect(zoomRange(histogram, range, 0, 60)).toEqual({ kind: 'absolute', ...range })
  })

  it('finds the bar under the pointer, and the end bars past the edges', () => {
    expect(barAt(0, 600, 60)).toBe(0)
    expect(barAt(305, 600, 60)).toBe(30)
    expect(barAt(599.9, 600, 60)).toBe(59)
    expect(barAt(600, 600, 60)).toBe(59)
    expect(barAt(-40, 600, 60)).toBe(0)
    expect(barAt(900, 600, 60)).toBe(59)
  })
})

function levels(counts: Partial<LevelCounts>): LevelCounts {
  return { trace: 0, debug: 0, info: 0, warn: 0, error: 0, fatal: 0, none: 0, ...counts }
}

describe('the summary line', () => {
  it('rates errors and fatals over every Log', () => {
    expect(errorRate({ count: 200, levels: levels({ info: 150, warn: 40, error: 6, fatal: 4 }) })).toBe(0.05)
  })

  it('counts Logs with no Level in the whole but never as errors', () => {
    expect(errorRate({ count: 4, levels: levels({ error: 1, none: 3 }) })).toBe(0.25)
  })

  it('has no error rate without Logs', () => {
    expect(errorRate({ count: 0, levels: levels({}) })).toBeNull()
  })

  it('filters on a clicked Level with a Level chip', () => {
    const builder = filterByLevel({ filters: [{ field: 'service', op: '==', value: 'api' }], search: 'boom' }, 'error')

    expect(builder && queryOf(builder)).toBe('where service == "api"\n| where level == "error"\n| search "boom"')
  })

  it('switches from one clicked Level to another', () => {
    const builder = filterByLevel(filterByLevel(EMPTY_BUILDER, 'warn')!, 'fatal')

    expect(builder && queryOf(builder)).toBe('where level == "fatal"')
  })

  it("can't filter on Logs with no Level", () => {
    expect(filterByLevel(EMPTY_BUILDER, 'none')).toBeNull()
  })
})
