import { describe, expect, it } from 'vitest'
import { binSize } from './bins'

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
