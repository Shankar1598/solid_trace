import { describe, expect, it } from 'vitest'
import { errorParts } from './errorParts'

// ClickHouse's messages, as it wrote them for this SQL.
const expected = 'Expected one of: FINAL, SAMPLE, table, table function, subquery or list of joined tables, end of query.'

describe('the parts of a run error', () => {
  it('points at the position ClickHouse gives, under the SQL\'s line', () => {
    const sql = "SELECT message FROM logs WHER level = 'error'"
    const message = `Syntax error: failed at position 31 (level): level = 'error'. ${expected}`

    expect(errorParts(message, sql)).toEqual([
      { kind: 'prose', text: message },
      {
        kind: 'pointer',
        text: "LINE 1: SELECT message FROM logs WHER level = 'error'\n"
          + '                                      ^',
      },
    ])
  })

  it('counts ClickHouse\'s position in bytes, across lines', () => {
    const sql = "SELECT 'é', message\nFROM logs\nWHERE level = 'error' ORDR BY time"
    const message = `Syntax error: failed at position 54 (ORDR) (line 3, col 23): ORDR BY time. ${expected}`

    expect(errorParts(message, sql)).toEqual([
      { kind: 'prose', text: message },
      {
        kind: 'pointer',
        text: "LINE 3: WHERE level = 'error' ORDR BY time\n"
          + '                              ^',
      },
    ])
  })

  it('points past the end at the end of the query, and keeps tabs before the caret', () => {
    const sql = 'SELECT *\n\tFROM logs WHERE'
    const message = `Syntax error: failed at position 26 (end of query): . ${expected}`

    expect(errorParts(message, sql)).toEqual([
      { kind: 'prose', text: message },
      { kind: 'pointer', text: 'LINE 2: \tFROM logs WHERE\n        \t               ^' },
    ])
  })

  it('is all prose without a position, or without the SQL', () => {
    const unknown = 'Unknown expression identifier `nope` in scope SELECT nope FROM logs.'
    expect(errorParts(unknown, 'SELECT nope FROM logs')).toEqual([{ kind: 'prose', text: unknown }])
    expect(errorParts('EventStore is unavailable. Try again shortly.'))
      .toEqual([{ kind: 'prose', text: 'EventStore is unavailable. Try again shortly.' }])
    const syntax = `Syntax error: failed at position 8 (FORM): FORM logs. ${expected}`
    expect(errorParts(syntax)).toEqual([{ kind: 'prose', text: syntax }])
    expect(errorParts(syntax, 'SELECT')).toEqual([{ kind: 'prose', text: syntax }])
  })
})
