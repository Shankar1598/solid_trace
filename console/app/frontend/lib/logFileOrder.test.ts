import { describe, expect, it } from 'vitest'
import { addField, offeredFields, orderSummary, whyNotAdded } from './logFileOrder'

const DEFAULT = ['service', 'file']

describe('orderSummary', () => {
  it('names the default while the Project sets no order', () => {
    expect(orderSummary(null, DEFAULT)).toBe('Default (service, file)')
  })

  it('says time only for an empty order', () => {
    expect(orderSummary([], DEFAULT)).toBe('Time only')
  })

  it('names the Fields of an order, then time', () => {
    expect(orderSummary(['level', 'http.route'], DEFAULT)).toBe('level, http.route, then time')
  })
})

describe('offeredFields', () => {
  it('offers service, environment, level and the string Attributes the order lacks', () => {
    expect(offeredFields(['service'], ['service', 'environment', 'level'], ['file', 'http.route', 'level']))
      .toEqual(['environment', 'level', 'file', 'http.route'])
  })
})

describe('addField', () => {
  it('adds a typed name to the end, trimmed', () => {
    expect(addField(['service'], '  file ')).toEqual(['service', 'file'])
  })

  it('leaves the order as it is for a blank name', () => {
    expect(addField(['service'], '   ')).toEqual(['service'])
  })
})

describe('whyNotAdded', () => {
  it('refuses a fourth Field', () => {
    expect(whyNotAdded(['service', 'file', 'level'], 'environment')).toBe('A Log file order has at most 3 Fields')
  })

  it('refuses a Field the order names already, trimmed', () => {
    expect(whyNotAdded(['service', 'file'], ' file ')).toBe('file is in the order already')
  })

  it('adds any other name', () => {
    expect(whyNotAdded(['service', 'file'], 'level')).toBeNull()
  })
})
