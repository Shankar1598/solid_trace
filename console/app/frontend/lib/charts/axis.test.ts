import { describe, expect, it } from 'vitest'
import { statText, tickLabel, timePattern, timeTicks, valueText, valueTicks } from './axis'

const at = (time: string) => new Date(time).getTime()

describe('time labels', () => {
  it('show local times to the bin\'s precision, with the date once the range spans days', () => {
    const day = { from: new Date(2026, 9, 9, 10), to: new Date(2026, 9, 9, 16) }
    const days = { from: new Date(2026, 9, 8, 10), to: new Date(2026, 9, 9, 16) }

    expect(timePattern(300, day)).toBe('HH:mm')
    expect(timePattern(10, day)).toBe('HH:mm:ss')
    expect(timePattern(300, days)).toBe('MMM dd HH:mm')
  })
})

describe('time ticks', () => {
  it('fall on whole steps of the local clock, the smallest step that gives at most max', () => {
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
    expect(valueTicks(0, 2859)).toEqual({ domain: [0, 3000], ticks: [0, 1000, 2000, 3000], step: 1000 })
    expect(valueTicks(12, 2200)).toMatchObject({ domain: [0, 2500], ticks: [0, 500, 1000, 1500, 2000, 2500] })
    expect(valueTicks(0, 7085).ticks).toEqual([0, 2000, 4000, 6000, 8000])
  })

  it('keep small values readable', () => {
    expect(valueTicks(0, 0.034)).toEqual({ domain: [0, 0.04], ticks: [0, 0.01, 0.02, 0.03, 0.04], step: 0.01 })
    expect(valueTicks(0, 7).ticks).toEqual([0, 2, 4, 6, 8])
  })

  it('take fewer steps for a short chart', () => {
    expect(valueTicks(0, 2859, 3).ticks).toEqual([0, 1000, 2000, 3000])
    expect(valueTicks(0, 820, 3).ticks).toEqual([0, 500, 1000])
  })

  it('reach below zero for negative values', () => {
    expect(valueTicks(-40, 90)).toMatchObject({ domain: [-50, 100], ticks: [-50, 0, 50, 100] })
  })

  it('give a flat or empty chart a scale of its own', () => {
    expect(valueTicks(0, 0)).toMatchObject({ domain: [0, 1], ticks: [0, 0.2, 0.4, 0.6, 0.8, 1] })
    expect(valueTicks(Infinity, -Infinity).domain).toEqual([0, 1])
  })
})

describe('value labels', () => {
  /** The labels a value axis shows for values up to max. */
  const labels = (max: number) => {
    const { ticks, step } = valueTicks(0, max)
    return ticks.map(tickLabel(step))
  }

  it('read each tick exactly, with the decimals its step needs', () => {
    expect(labels(1240)).toEqual(['0', '250', '500', '750', '1K', '1.25K'])
    expect(labels(1.1)).toEqual(['0', '0.25', '0.5', '0.75', '1', '1.25'])
    expect(labels(0.2)).toEqual(['0', '0.05', '0.1', '0.15', '0.2'])
    expect(labels(0.034)).toEqual(['0', '0.01', '0.02', '0.03', '0.04'])
    expect(labels(0.12)).toEqual(['0', '0.025', '0.05', '0.075', '0.1', '0.125'])
  })

  it('go compact from a thousand up, without decimals a round step does not need', () => {
    expect(labels(2859)).toEqual(['0', '1K', '2K', '3K'])
    expect(labels(1_100_000)).toEqual(['0', '250K', '500K', '750K', '1M', '1.25M'])
    expect(labels(12_000)).toEqual(['0', '2.5K', '5K', '7.5K', '10K', '12.5K'])
  })

  it('never read two ticks the same', () => {
    for (const max of [0.0004, 0.034, 0.12, 1.1, 7, 1240, 33_000, 1_100_000]) {
      const read = labels(max)
      expect(new Set(read).size).toBe(read.length)
    }
  })

  // Compact units differ by locale: lakh and crore in India, 万 and 億 in
  // Japan. A tick reads exactly whatever unit the viewer's locale picks.
  const labelsIn = (locale: string, max: number) => {
    const { ticks, step } = valueTicks(0, max)
    return ticks.map(tickLabel(step, locale))
  }

  it('read each tick exactly in a locale whose units are not thousands', () => {
    expect(labelsIn('en-IN', 1_100_000)).toEqual(['0', '2.5L', '5L', '7.5L', '10L', '12.5L'])
    expect(labelsIn('en-IN', 110_000_000)).toEqual(['0', '2.5Cr', '5Cr', '7.5Cr', '10Cr', '12.5Cr'])
    expect(labelsIn('ja-JP', 12_000)).toEqual(['0', '2500', '5000', '7500', '1万', '1.25万'])
    expect(labelsIn('ja-JP', 110_000_000)).toEqual(['0', '2500万', '5000万', '7500万', '1億', '1.25億'])
  })

  it('never read two ticks the same in any locale', () => {
    for (const locale of ['en-US', 'en-IN', 'ja-JP', 'zh-CN', 'de-DE']) {
      for (const max of [0.0004, 0.034, 0.12, 1.1, 7, 1240, 12_000, 33_000, 1_100_000, 110_000_000]) {
        const read = labelsIn(locale, max)
        expect(new Set(read).size, `${locale} up to ${max}: ${read.join(' | ')}`).toBe(read.length)
      }
    }
  })
})

describe('values in a tooltip or a stat tile', () => {
  it('keep small values, and round others to two decimals', () => {
    expect(valueText(0.0004)).toBe('0.0004')
    expect(valueText(0.004123)).toBe('0.00412')
    expect(valueText(1234.5678)).toBe('1,234.57')
    expect(valueText(0)).toBe('0')
  })

  it('go compact in a stat tile from a million up', () => {
    expect(statText(19_183)).toBe('19,183')
    expect(statText(0.004)).toBe('0.004')
    expect(statText(4_213_000)).toBe('4.21M')
  })
})
