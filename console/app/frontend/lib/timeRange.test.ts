import { describe, expect, it } from 'vitest'
import {
  QUICK_RANGES,
  absoluteFromInputs,
  isSpan,
  localInput,
  longerThan,
  parseRange,
  rangeLabel,
  resolveRange,
  sameRange,
  spanLabel,
} from './timeRange'

// Local times, so the tests read the same in any time zone.
const now = new Date(2026, 9, 11, 15, 42, 30)
const relative = (span: string) => ({ kind: 'relative', span }) as const
const absolute = (from: Date, to: Date) => ({ kind: 'absolute', from, to }) as const

describe('typed ranges', () => {
  it('reads a number and a unit as a span back from now', () => {
    expect(parseRange('15m', now)).toEqual({ range: relative('15m') })
    expect(parseRange('2h', now)).toEqual({ range: relative('2h') })
    expect(parseRange('3d', now)).toEqual({ range: relative('3d') })
    expect(parseRange(' 90 mins ', now)).toEqual({ range: relative('90m') })
  })

  it('reads weeks as days, which the URL keeps', () => {
    expect(parseRange('1w', now)).toEqual({ range: relative('7d') })
    expect(parseRange('2 weeks', now)).toEqual({ range: relative('14d') })
  })

  it('reads words, with or without last and a number', () => {
    expect(parseRange('last 3 hours', now)).toEqual({ range: relative('3h') })
    expect(parseRange('Last 15 Minutes', now)).toEqual({ range: relative('15m') })
    expect(parseRange('past 2 days', now)).toEqual({ range: relative('2d') })
    expect(parseRange('last hour', now)).toEqual({ range: relative('1h') })
    expect(parseRange('last week', now)).toEqual({ range: relative('7d') })
    expect(parseRange('5 hrs', now)).toEqual({ range: relative('5h') })
  })

  it('reads today as midnight until now, and yesterday as the whole day before', () => {
    expect(parseRange('today', now)).toEqual({ range: absolute(new Date(2026, 9, 11), now) })
    expect(parseRange('yesterday', now)).toEqual({ range: absolute(new Date(2026, 9, 10), new Date(2026, 9, 11)) })
  })

  it('reads nothing from an empty box', () => {
    expect(parseRange('   ', now)).toBeNull()
  })

  it('says what it can read when it reads nothing else', () => {
    for (const text of ['soon', '0m', '15', 'h', '10s', 'last 3 fortnights']) {
      const parsed = parseRange(text, now)
      expect(parsed && 'error' in parsed, text).toBe(true)
    }
    expect(parseRange('soon', now)).toEqual({ error: 'Try 15m, 2h, 3d, 1w, last 3 hours, today or yesterday.' })
  })
})

describe('labels', () => {
  it('names a span back from now', () => {
    expect(spanLabel('6h')).toBe('Last 6 hours')
    expect(spanLabel('1h')).toBe('Last hour')
    expect(spanLabel('24h')).toBe('Last 24 hours')
    expect(spanLabel('15m')).toBe('Last 15 minutes')
    expect(spanLabel('1d')).toBe('Last day')
    expect(spanLabel('90d')).toBe('Last 90 days')
  })

  it('names an absolute range by its local times, the day once when it is one day', () => {
    expect(rangeLabel(absolute(new Date(2026, 9, 9, 10, 0), new Date(2026, 9, 9, 12, 30)), now)).toBe('Oct 9 10:00 – 12:30')
    expect(rangeLabel(absolute(new Date(2026, 9, 9, 22, 0), new Date(2026, 9, 10, 2, 0)), now)).toBe('Oct 9 22:00 – Oct 10 02:00')
  })

  it('shows seconds only when a time has them, and the year only when it is not this one', () => {
    expect(rangeLabel(absolute(new Date(2026, 9, 9, 10, 0, 15), new Date(2026, 9, 9, 10, 1, 45)), now)).toBe('Oct 9 10:00:15 – 10:01:45')
    expect(rangeLabel(absolute(new Date(2025, 11, 31, 23, 0), new Date(2026, 0, 1, 1, 0)), now)).toBe('Dec 31, 2025 23:00 – Jan 1 01:00')
  })

  it('names a relative range by its span', () => {
    expect(rangeLabel(relative('6h'), now)).toBe('Last 6 hours')
  })
})

describe('quick ranges', () => {
  it('runs from 5 minutes to 90 days, with 1 day the URL\'s 24h', () => {
    expect(QUICK_RANGES.map(q => q.label)).toEqual([
      '5 mins', '15 mins', '30 mins', '1 hr', '3 hrs', '6 hrs', '1 day', '2 days', '7 days', '15 days', '30 days', '90 days',
    ])
    expect(QUICK_RANGES.find(q => q.label === '1 day')?.span).toBe('24h')
    expect(QUICK_RANGES.every(q => isSpan(q.span))).toBe(true)
  })

  it('counts spans of the same length as the same range', () => {
    expect(sameRange(relative('24h'), relative('1d'))).toBe(true)
    expect(sameRange(relative('1h'), relative('60m'))).toBe(true)
    expect(sameRange(relative('1h'), relative('2h'))).toBe(false)
    expect(sameRange(relative('1h'), absolute(new Date(now.getTime() - 3_600_000), now))).toBe(false)
    expect(sameRange(absolute(new Date(1), new Date(2)), absolute(new Date(1), new Date(2)))).toBe(true)
  })
})

describe('Retention', () => {
  it('turns away ranges longer than the days a Project keeps its Logs', () => {
    expect(longerThan(relative('30d'), 30)).toBe(false)
    expect(longerThan(relative('90d'), 30)).toBe(true)
    expect(longerThan(relative('721h'), 30)).toBe(true)
    expect(longerThan(absolute(new Date(2026, 8, 1), new Date(2026, 9, 11)), 30)).toBe(true)
    expect(longerThan(absolute(new Date(2026, 9, 1), new Date(2026, 9, 11)), 30)).toBe(false)
  })

  it('turns away nothing without a Retention', () => {
    expect(longerThan(relative('90d'), undefined)).toBe(false)
  })
})

describe('start and end dates', () => {
  it('writes a time as a local date-time input reads it', () => {
    expect(localInput(new Date(2026, 9, 9, 7, 5, 59))).toBe('2026-10-09T07:05')
  })

  it('reads two local date-times as an absolute range', () => {
    expect(absoluteFromInputs('2026-10-09T10:00', '2026-10-09T12:30')).toEqual({
      range: absolute(new Date(2026, 9, 9, 10, 0), new Date(2026, 9, 9, 12, 30)),
    })
  })

  it('needs both, and the start before the end', () => {
    expect(absoluteFromInputs('', '2026-10-09T12:30')).toEqual({ error: 'Pick a start and an end.' })
    expect(absoluteFromInputs('2026-10-09T12:30', '2026-10-09T10:00')).toEqual({ error: 'The start must be before the end.' })
    expect(absoluteFromInputs('2026-10-09T10:00', '2026-10-09T10:00')).toEqual({ error: 'The start must be before the end.' })
  })
})

describe('resolving a range', () => {
  it('ends a relative range now and keeps an absolute one', () => {
    expect(resolveRange(relative('15m'), now)).toEqual({ from: new Date(now.getTime() - 15 * 60_000), to: now })
    const range = absolute(new Date(2026, 9, 9), new Date(2026, 9, 10))
    expect(resolveRange(range, now)).toEqual({ from: range.from, to: range.to })
  })
})
