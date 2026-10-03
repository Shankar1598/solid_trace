import { describe, expect, it } from 'vitest'
import { bucketSeconds } from './histogram'

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
