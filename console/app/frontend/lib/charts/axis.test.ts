import { describe, expect, it } from 'vitest'
import { timePattern, timeTicks, valueTicks } from './axis'

const at = (time: string) => new Date(time).getTime()

describe('time labels', () => {
  it('show local times to the bin\'s precision, with the date once the range spans days', () => {
    const day = { from: new Date(2026, 9, 9, 10), to: new Date(2026, 9, 9, 16) }
    const days = { from: new Date(2026, 9, 8, 10), to: new Date(2026, 9, 9, 16) }

    expect(timePattern(300, day)).toBe('HH:mm')
    expect(timePattern(10, day)).toBe('HH:mm:ss')
    expect(timePattern(300, days)).toBe('MMM dd HH:mm')
    expect(timePattern(86_400, days)).toBe('MMM dd')
  })
})

describe('time ticks', () => {
  it('fall on whole steps from the epoch, the smallest step that gives at most max', () => {
    const ticks = timeTicks(at('2026-10-09T10:03:00Z'), at('2026-10-09T11:03:00Z'), 4)

    expect(ticks.map(t => new Date(t).toISOString())).toEqual([
      '2026-10-09T10:15:00.000Z', '2026-10-09T10:30:00.000Z', '2026-10-09T10:45:00.000Z', '2026-10-09T11:00:00.000Z',
    ])
  })

  it('include an end that falls on a step', () => {
    expect(timeTicks(at('2026-10-09T10:00:00Z'), at('2026-10-09T10:05:00Z'), 6)).toContain(at('2026-10-09T10:05:00Z'))
  })

  it('fall on local midnights once a step is a day or more', () => {
    const ticks = timeTicks(new Date(2026, 9, 1, 13).getTime(), new Date(2026, 9, 8, 13).getTime(), 8)

    expect(ticks.map(t => new Date(t))).toEqual([2, 3, 4, 5, 6, 7, 8].map(d => new Date(2026, 9, d)))
  })

  it('are none for an empty range', () => {
    expect(timeTicks(5, 5, 6)).toEqual([])
  })
})

describe('value ticks', () => {
  it('fall on round numbers from zero past the largest value', () => {
    expect(valueTicks(0, 2859)).toEqual({ domain: [0, 3000], ticks: [0, 1000, 2000, 3000] })
    expect(valueTicks(12, 2200)).toEqual({ domain: [0, 2500], ticks: [0, 500, 1000, 1500, 2000, 2500] })
    expect(valueTicks(0, 7085).ticks).toEqual([0, 2000, 4000, 6000, 8000])
  })

  it('keep small values readable', () => {
    expect(valueTicks(0, 0.034)).toEqual({ domain: [0, 0.04], ticks: [0, 0.01, 0.02, 0.03, 0.04] })
    expect(valueTicks(0, 7).ticks).toEqual([0, 2, 4, 6, 8])
  })

  it('reach below zero for negative values', () => {
    expect(valueTicks(-40, 90)).toEqual({ domain: [-50, 100], ticks: [-50, 0, 50, 100] })
  })

  it('give a flat or empty chart a scale of its own', () => {
    expect(valueTicks(0, 0)).toEqual({ domain: [0, 1], ticks: [0, 0.2, 0.4, 0.6, 0.8, 1] })
    expect(valueTicks(Infinity, -Infinity).domain).toEqual([0, 1])
  })
})
