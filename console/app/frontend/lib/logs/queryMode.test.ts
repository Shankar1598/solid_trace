import { describe, expect, it } from 'vitest'
import type { QueryStage, Validation } from './api'
import { pivotQuery, toBuilder } from './queryMode'

// Stages as validate places them: from each stage's keyword to its end.
function stage(line: number, column: number, endLine: number, endColumn: number, filter: boolean, builder = false): QueryStage {
  return { line, column, end_line: endLine, end_column: endColumn, filter, builder }
}

describe('switching from Query to Builder', () => {
  it('switches silently when validate gives chips', () => {
    const validation: Validation = {
      errors: [],
      chips: { filters: [{ field: 'service', op: '==', value: 'api' }], search: 'timeout' },
      stages: [stage(1, 1, 1, 23, true, true), stage(1, 26, 1, 42, true, true)],
    }

    expect(toBuilder('where service == "api" | search "timeout"', validation)).toEqual({
      kind: 'switch',
      builder: { filters: [{ field: 'service', op: '==', value: 'api' }], search: 'timeout' },
    })
  })

  it('names the stages the Builder can\'t show, and asks', () => {
    const query = 'logs\n| where service == "api"\n| where duration > 500\n| summarize count() by service'
    const validation: Validation = {
      errors: [],
      chips: null,
      stages: [stage(2, 3, 2, 25, true, true), stage(3, 3, 3, 23, true), stage(4, 3, 4, 31, false)],
    }

    expect(toBuilder(query, validation)).toEqual({
      kind: 'ask',
      stages: ['where duration > 500', 'summarize count() by service'],
      problems: [],
    })
  })

  it('counts columns in characters, as validate does', () => {
    const validation: Validation = {
      errors: [],
      chips: null,
      stages: [stage(1, 1, 1, 11, true, true), stage(1, 14, 1, 24, true)],
    }

    expect(toBuilder('search "😀" | search "b"', validation)).toMatchObject({ stages: ['search "b"'] })
  })

  it('asks with the mistakes when the Query has some', () => {
    const problem = { message: 'unknown stage "bogus"', line: 1, column: 21, end_line: 1, end_column: 26 }

    expect(toBuilder('where user == "x" | bogus', { errors: [problem], chips: null, stages: [] })).toEqual({
      kind: 'ask',
      stages: [],
      problems: [problem],
    })
  })
})

describe('a pivot in Query mode', () => {
  const show = { field: 'service', op: '==', value: 'api' } as const

  it('adds its where after the leading filters, before a summarize', () => {
    const query = 'logs\n| where level == "error"\n| summarize count() by service'
    const stages = [stage(2, 3, 2, 25, true, true), stage(3, 3, 3, 31, false)]

    expect(pivotQuery(query, stages, show)).toBe(
      'logs\n| where level == "error"\n| where service == "api"\n| summarize count() by service',
    )
  })

  it('goes first when the Query starts with a summarize', () => {
    const query = 'logs | summarize count() by service'
    const stages = [stage(1, 8, 1, 36, false)]

    expect(pivotQuery(query, stages, { field: 'http.status', op: '!=', value: 500 })).toBe(
      'logs | where http.status != 500\n| summarize count() by service',
    )
  })

  it('goes last when every stage is a filter', () => {
    const stages = [stage(1, 1, 1, 21, true), stage(1, 24, 1, 34, true, true)]

    expect(pivotQuery('where duration > 500 | search "x"', stages, show)).toBe(
      'where duration > 500 | search "x"\n| where service == "api"',
    )
  })

  it('is the whole Query when there was none', () => {
    expect(pivotQuery('', [], show)).toBe('where service == "api"')
    expect(pivotQuery('logs', [], show)).toBe('logs\n| where service == "api"')
  })

  it('places itself by characters after a non-ASCII filter', () => {
    const query = 'where user == "😀" | count'
    const stages = [stage(1, 1, 1, 18, true, true), stage(1, 21, 1, 26, false)]

    expect(pivotQuery(query, stages, show)).toBe('where user == "😀"\n| where service == "api" | count')
  })
})
