import { describe, expect, it } from 'vitest'
import { pivotsOn } from './table'

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
