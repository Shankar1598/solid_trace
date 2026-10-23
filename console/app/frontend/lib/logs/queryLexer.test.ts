import { describe, expect, it } from 'vitest'
import { parametersOf, readToken, startState, tokenize, type TokenKind } from './queryLexer'

const kinds = (text: string) => tokenize(text).map(t => [t.text, t.kind] as [string, TokenKind])

describe('colouring a Query', () => {
  it('tells stages from Fields, functions and operators', () => {
    expect(kinds(`['my-api'] | where level == "error" and status >= 500`)).toEqual([
      ["['my-api']", 'name'], ['|', 'pipe'], ['where', 'stage'], ['level', 'field'], ['==', 'operator'], ['"error"', 'string'],
      ['and', 'operator'], ['status', 'field'], ['>=', 'operator'], ['500', 'number'],
    ])
  })

  it('reads count as a stage after |, and as a function in summarize', () => {
    expect(kinds('summarize count() by service | count')).toEqual([
      ['summarize', 'stage'], ['count', 'function'], ['(', 'punctuation'], [')', 'punctuation'], ['by', 'keyword'],
      ['service', 'field'], ['|', 'pipe'], ['count', 'stage'],
    ])
  })

  it('reads timespans, numbers, bools and datetimes', () => {
    expect(kinds('bin(time, 5m) 1.5 1e3 100ms true datetime(2026-10-02T10:00:00Z) 5q')).toEqual([
      ['bin', 'function'], ['(', 'punctuation'], ['time', 'field'], [',', 'punctuation'], ['5m', 'timespan'], [')', 'punctuation'],
      ['1.5', 'number'], ['1e3', 'number'], ['100ms', 'timespan'], ['true', 'literal'],
      ['datetime(2026-10-02T10:00:00Z)', 'literal'], ['5q', 'invalid'],
    ])
  })

  it('reads negated word operators as one', () => {
    expect(kinds('where message !contains "x" and service !in ("a")').map(([text, kind]) => `${text}:${kind}`)).toContain('!contains:operator')
    expect(kinds('service !in ("a")')[1]).toEqual(['!in', 'operator'])
    expect(kinds('service in ("a")')[1]).toEqual(['in', 'operator'])
  })

  it('reads comments to the end of the line, and strings with escapes', () => {
    expect(kinds('where message == "say \\"hi\\"" // why\n| take 5')).toEqual([
      ['where', 'stage'], ['message', 'field'], ['==', 'operator'], ['"say \\"hi\\""', 'string'], ['// why', 'comment'],
      ['|', 'pipe'], ['take', 'stage'], ['5', 'number'],
    ])
  })

  it('opens a stage on the line after a |', () => {
    expect(kinds("logs |\nsort by time desc nulls last")).toEqual([
      ['logs', 'name'], ['|', 'pipe'], ['sort', 'stage'], ['by', 'keyword'], ['time', 'field'], ['desc', 'keyword'],
      ['nulls', 'keyword'], ['last', 'keyword'],
    ])
  })

  it('gives unterminated strings and names the rest of the line', () => {
    expect(kinds('where a == "open\n| take 1')[3]).toEqual(['"open', 'string'])
    expect(kinds("['open")).toEqual([["['open", 'name']])
  })

  it('always moves on, one line at a time', () => {
    const state = startState()
    const line = 'where a == ¿ 5'
    let pos = 0
    let steps = 0
    while (pos < line.length) {
      const { end } = readToken(line, pos, state)
      expect(end).toBeGreaterThan(pos)
      pos = end
      steps++
    }
    expect(steps).toBeLessThan(20)
  })

  it('reads $name as a Dashboard filter, and a Field named $type in brackets', () => {
    expect(kinds(`where service == $service | search $q`)).toEqual([
      ['where', 'stage'], ['service', 'field'], ['==', 'operator'], ['$service', 'parameter'], ['|', 'pipe'],
      ['search', 'stage'], ['$q', 'parameter'],
    ])
    expect(kinds(`where ['$type'] == $type`)).toEqual([
      ['where', 'stage'], ["['$type']", 'name'], ['==', 'operator'], ['$type', 'parameter'],
    ])
    // $ is still part of a name, but no longer starts one.
    expect(kinds('a$b $ $1')).toEqual([['a$b', 'field'], ['$', 'invalid'], ['$', 'invalid'], ['1', 'number']])
  })
})

describe('the Dashboard filters a Query names', () => {
  it('lists each name once, in order of first use', () => {
    expect(parametersOf('where service == $service\n| where env != $env or service =~ $service\n| search $q')).toEqual([
      'service', 'env', 'q',
    ])
  })

  it('leaves out $ in strings, bracketed names and comments', () => {
    expect(parametersOf(`where message == "$a" and ['$b'] == 1 // $c\n| where level == $level`)).toEqual(['level'])
    expect(parametersOf('where level == "error"')).toEqual([])
  })
})
