import { describe, expect, it } from 'vitest'
import { numberText, pivotsOn } from './table'

describe('pivoting from a table cell', () => {
  it('is offered on Fields grouped or listed by name: strings, bools and dynamic values', () => {
    expect(pivotsOn({ name: 'service', type: 'string' })).toBe(true)
    expect(pivotsOn({ name: 'http.status', type: 'dynamic' })).toBe(true)
    expect(pivotsOn({ name: 'cached', type: 'bool' })).toBe(true)
  })

  it('isn\'t offered on numbers, which are mostly aggregates no Log has', () => {
    expect(pivotsOn({ name: 'count_', type: 'number' })).toBe(false)
    expect(pivotsOn({ name: 'avg_duration', type: 'number' })).toBe(false)
  })

  it('isn\'t offered on times and timespans, which the time range filters', () => {
    expect(pivotsOn({ name: 'time', type: 'datetime' })).toBe(false)
    expect(pivotsOn({ name: 'took', type: 'timespan' })).toBe(false)
  })
})

describe('a number in a table cell', () => {
  it('reads an aggregate as a chart does: grouped, to two decimals', () => {
    expect(numberText({ name: 'avg_duration_ms', type: 'number', agg: 'avg' }, 544.2391475211607)).toBe('544.24')
    expect(numberText({ name: 'count_', type: 'number', agg: 'count' }, 4962)).toBe('4,962')
    expect(numberText({ name: 'max_duration_ms', type: 'number', agg: 'max' }, 8991.7)).toBe('8,991.7')
  })

  it('keeps three significant digits of an aggregate under one', () => {
    expect(numberText({ name: 'avg_ratio', type: 'number', agg: 'avg' }, 0.0123456)).toBe('0.0123')
  })

  it('shows any other number exactly as the Log holds it, as an ID or status is', () => {
    expect(numberText({ name: 'charge.number', type: 'number' }, 9007199254741604)).toBe('9007199254741604')
    expect(numberText({ name: 'ratio', type: 'number' }, 0.1234567)).toBe('0.1234567')
  })
})
