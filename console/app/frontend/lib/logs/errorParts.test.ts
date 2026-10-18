import { describe, expect, it } from 'vitest'
import { errorParts } from './errorParts'

describe('the parts of a run error', () => {
  it('keeps DuckDB\'s LINE and caret together, apart from the prose around them', () => {
    const message = [
      'Binder Error: Referenced column "nope" not found in FROM clause!',
      'Candidate bindings: "project_id", "environment", "time"',
      '',
      'LINE 1: SELECT service, count(*) AS n FROM logs WHERE nope = 1 GROUP BY service',
      '                                                      ^',
    ].join('\n')

    expect(errorParts(message)).toEqual([
      {
        kind: 'prose',
        text: 'Binder Error: Referenced column "nope" not found in FROM clause!\nCandidate bindings: "project_id", "environment", "time"',
      },
      {
        kind: 'pointer',
        text: 'LINE 1: SELECT service, count(*) AS n FROM logs WHERE nope = 1 GROUP BY service\n'
          + '                                                      ^',
      },
    ])
  })

  it('keeps prose after the caret', () => {
    expect(errorParts('Parser Error: syntax error at or near "FORM"\n\nLINE 1: SELECT * FORM logs\n                 ^\nTry again.'))
      .toEqual([
        { kind: 'prose', text: 'Parser Error: syntax error at or near "FORM"' },
        { kind: 'pointer', text: 'LINE 1: SELECT * FORM logs\n                 ^' },
        { kind: 'prose', text: 'Try again.' },
      ])
  })

  it('is all prose without a caret', () => {
    expect(errorParts('EventStore is unavailable. Try again shortly.'))
      .toEqual([{ kind: 'prose', text: 'EventStore is unavailable. Try again shortly.' }])
    expect(errorParts('LINE 1: is just words here'))
      .toEqual([{ kind: 'prose', text: 'LINE 1: is just words here' }])
  })
})
