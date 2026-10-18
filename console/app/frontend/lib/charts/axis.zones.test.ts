import { afterEach, describe, expect, it } from 'vitest'
import { format } from 'date-fns'
import { timePattern, timeTicks } from './axis'

// Time axes in zones whose offset is not whole hours (Kolkata, +5:30) or is
// behind UTC (New York): ticks fall on round local times, and day bins,
// which start at UTC midnights, say the local time they start at.

const at = (time: string) => new Date(time).getTime()
const zone = process.env.TZ

/** Runs in a zone; Node reads TZ afresh each time it is set. */
function inZone(tz: string) {
  process.env.TZ = tz
}

afterEach(() => {
  if (zone === undefined) delete process.env.TZ
  else process.env.TZ = zone
})

const local = (ticks: number[]) => ticks.map(t => format(t, 'HH:mm'))

describe('time ticks in a zone with a half-hour offset', () => {
  it('fall on local hours the step divides', () => {
    inZone('Asia/Kolkata')
    expect(local(timeTicks(at('2026-10-09T10:07:00Z'), at('2026-10-10T10:07:00Z'), 6))).toEqual(['18:00', '00:00', '06:00', '12:00'])
    expect(local(timeTicks(at('2026-10-09T04:07:00Z'), at('2026-10-09T10:07:00Z'), 6))).toEqual(['10:00', '11:00', '12:00', '13:00', '14:00', '15:00'])
  })

  it('fall on whole local steps under an hour', () => {
    inZone('Asia/Kolkata')
    expect(local(timeTicks(at('2026-10-09T10:07:00Z'), at('2026-10-09T11:07:00Z'), 4))).toEqual(['15:45', '16:00', '16:15', '16:30'])
  })
})

describe('time ticks behind UTC', () => {
  it('fall on local hours the step divides', () => {
    inZone('America/New_York')
    expect(local(timeTicks(at('2026-10-09T10:07:00Z'), at('2026-10-10T10:07:00Z'), 6))).toEqual(['12:00', '18:00', '00:00', '06:00'])
  })
})

describe('day bins away from UTC', () => {
  const range = { from: new Date('2026-09-01T00:00:00Z'), to: new Date('2026-10-09T00:00:00Z') }

  it('show the local time they start at, so a bar never reads as the day before its tick', () => {
    inZone('America/New_York')
    expect(timePattern(86_400, range)).toBe('MMM dd HH:mm')
    expect(format(at('2026-10-09T00:00:00Z'), timePattern(86_400, range))).toBe('Oct 08 20:00')
  })

  it('show only the date in UTC', () => {
    inZone('UTC')
    expect(timePattern(86_400, range)).toBe('MMM dd')
  })
})
