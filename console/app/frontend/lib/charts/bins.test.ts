import { describe, expect, it } from 'vitest'
import { binSize, binSpan } from './bins'

const at = (...parts: [number, number, number, number?, number?, number?]) => new Date(...parts).getTime()

describe('bin size', () => {
  it('reads in the largest unit it is a whole number of, from a second to a week', () => {
    expect(binSize(1)).toBe('1s')
    expect(binSize(10)).toBe('10s')
    expect(binSize(30)).toBe('30s')
    expect(binSize(60)).toBe('1m')
    expect(binSize(300)).toBe('5m')
    expect(binSize(3_600)).toBe('1h')
    expect(binSize(43_200)).toBe('12h')
    expect(binSize(86_400)).toBe('1d')
    expect(binSize(604_800)).toBe('7d')
  })

  it('falls back to a smaller unit when it is not a whole number of the larger', () => {
    expect(binSize(90)).toBe('90s')
    expect(binSize(5_400)).toBe('90m')
    expect(binSize(129_600)).toBe('36h')
  })

  it('reads a bin under a second in milliseconds', () => {
    expect(binSize(0.5)).toBe('500ms')
    expect(binSize(1.5)).toBe('1500ms')
  })
})

describe('bin span', () => {
  it('reads from the bin\'s start to its end in local time, to the axis\' precision', () => {
    expect(binSpan(at(2026, 9, 9, 19, 2), 60, 'HH:mm')).toBe('19:02–19:03')
    expect(binSpan(at(2026, 9, 9, 19, 2, 10), 10, 'HH:mm:ss')).toBe('19:02:10–19:02:20')
  })

  it('gives the date once when the bin starts and ends on the same day', () => {
    expect(binSpan(at(2026, 9, 9, 10), 3_600, 'MMM dd HH:mm')).toBe('Oct 09 10:00–11:00')
    expect(binSpan(at(2026, 9, 9, 23), 3_600, 'MMM dd HH:mm')).toBe('Oct 09 23:00–Oct 10 00:00')
  })

  it('gives both days for a bin of days', () => {
    expect(binSpan(at(2026, 9, 9), 86_400, 'MMM dd')).toBe('Oct 09–Oct 10')
    expect(binSpan(at(2026, 9, 5), 604_800, 'MMM dd')).toBe('Oct 05–Oct 12')
  })
})
