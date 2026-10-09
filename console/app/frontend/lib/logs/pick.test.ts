import { describe, expect, it } from 'vitest'
import { fieldOptions, listFragment, narrow, toggleListValue, valueOptions } from './pick'

describe('narrowing a pick list', () => {
  const options = [
    { value: 'bin:auto', text: 'bin_auto(time)', group: 'Time' },
    { value: 'bin:5m', text: 'bin(time, 5m)', group: 'Time' },
    { value: 'user.service', group: 'Fields' },
    { value: 'service', group: 'Fields' },
    { value: 'http.status', group: 'Fields' },
  ]

  it('keeps every option while nothing is typed', () => {
    expect(narrow(options, '  ')).toEqual(options)
  })

  it('keeps the options containing the text, ignoring case, those starting with it first in their group', () => {
    expect(narrow(options, 'SERV').map(o => o.value)).toEqual(['service', 'user.service'])
  })

  it('matches an option by its text when it has one, and keeps the groups in order', () => {
    expect(narrow(options, 'time').map(o => o.value)).toEqual(['bin:auto', 'bin:5m'])
    expect(narrow(options, 's').map(o => o.value)).toEqual(['service', 'user.service', 'http.status'])
  })
})

describe('the Fields to pick', () => {
  const fields = [
    { name: 'time', type: 'time' },
    { name: 'level', type: 'string' },
    { name: 'duration_ms', type: 'number' },
    { name: 'cache.hit', type: 'bool' },
  ]

  it('leaves time out, and puts the built-in fields before the Attributes', () => {
    expect(fieldOptions(fields)).toEqual([
      { value: 'level', hint: 'string', group: 'Built-in' },
      { value: 'duration_ms', hint: 'number', group: 'Attributes' },
      { value: 'cache.hit', hint: 'bool', group: 'Attributes' },
    ])
  })

  it('puts the preferred types first', () => {
    expect(fieldOptions(fields, { types: ['number'], group: 'Numbers' }).map(o => `${o.group}:${o.value}`))
      .toEqual(['Numbers:duration_ms', 'Other Fields:level', 'Other Fields:cache.hit'])
  })
})

describe('the values to pick', () => {
  it('writes values to read back with their type, or as typed for the text operators', () => {
    expect(valueOptions(['api', '500', 200, true], false)).toEqual([
      { value: 'api', hint: 'string' },
      { value: '"500"', hint: 'string' },
      { value: '200', hint: 'number' },
      { value: 'true', hint: 'bool' },
    ])
    expect(valueOptions(['500'], true)).toEqual([{ value: '500', hint: 'string' }])
  })

  it('finds the part of a list still being typed', () => {
    const known = ['500', '502']
    expect(listFragment('500, 5', known)).toBe('5')
    expect(listFragment('500, 502', known)).toBe('')
    expect(listFragment('50', known)).toBe('50')
    expect(listFragment('500,', known)).toBe('')
  })

  it('adds a value to a list in place of the part being typed, and takes it out again', () => {
    const known = ['500', '502', '503']
    expect(toggleListValue('', '500', known)).toBe('500')
    expect(toggleListValue('500, 50', '502', known)).toBe('500, 502')
    expect(toggleListValue('500, 502', '503', known)).toBe('500, 502, 503')
    expect(toggleListValue('500, 502, 503', '502', known)).toBe('500, 503')
  })
})
