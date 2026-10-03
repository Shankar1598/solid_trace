import { describe, expect, it } from 'vitest'
import { openExplorer, runExplorer, searchOf, typeQuery, typeSql } from './explorerState'
import { discardSql, enterSql, leaveSql } from './sqlMode'

const SEED = 'WITH stage1 AS (\n  SELECT * FROM logs WHERE "level" = \'error\'\n)\nSELECT * FROM stage1 ORDER BY "time" DESC, "id" DESC'

describe('entering SQL mode', () => {
  it('opens with the readable SQL of the Query, in the URL in place of the current entry', () => {
    const explorer = typeQuery(openExplorer('?range=1h&mode=query&q=where+level+%3D%3D+%22error%22'), 'where level == "error"')

    const { explorer: next, replace } = enterSql(explorer, SEED)

    expect(next.current).toMatchObject({ mode: 'sql', query: 'where level == "error"', sql: SEED })
    expect(next.draft).toMatchObject({ mode: 'sql', sql: SEED })
    expect(replace).toBe(searchOf(next.current))
    expect(openExplorer(replace).current.sql).toBe(SEED)
  })
})

describe('leaving SQL mode', () => {
  const inSql = () => enterSql(openExplorer('?range=1h&mode=builder&q=search+%22boom%22'), SEED).explorer

  it('switches silently when the SQL is as it opened, dropping it from the URL', () => {
    const answer = leaveSql(inSql(), SEED, 'builder')

    expect(answer.kind).toBe('switch')
    if (answer.kind !== 'switch') return
    expect(answer.explorer.current).toMatchObject({ mode: 'builder', query: 'search "boom"', sql: '' })
    expect(answer.explorer.draft).toMatchObject({ mode: 'builder', sql: '' })
    expect(answer.replace).toBe('?range=1h&mode=builder&q=search+%22boom%22')
  })

  it('switches silently after running the SQL as it opened', () => {
    const ran = runExplorer(inSql()).explorer

    expect(leaveSql(ran, SEED, 'query').kind).toBe('switch')
  })

  it('asks before discarding edited SQL, typed or run', () => {
    const typed = typeSql(inSql(), `${SEED} LIMIT 5`)
    const ran = runExplorer(typed).explorer

    expect(leaveSql(typed, SEED, 'query')).toEqual({ kind: 'ask', mode: 'query' })
    expect(leaveSql(ran, SEED, 'builder')).toEqual({ kind: 'ask', mode: 'builder' })
  })

  it('asks before discarding SQL opened from a link, which it did not write', () => {
    const fromLink = openExplorer('?range=1h&mode=sql&q=take+5&sql=SELECT+1')

    expect(leaveSql(fromLink, null, 'query').kind).toBe('ask')
  })

  it('discards the SQL once asked, going back to the Query it was opened from', () => {
    const edited = typeSql(inSql(), 'SELECT 1')

    const { explorer, replace } = discardSql(edited, 'query')

    expect(explorer.current).toMatchObject({ mode: 'query', query: 'search "boom"', sql: '' })
    expect(explorer.draft).toMatchObject({ mode: 'query', query: 'search "boom"', sql: '' })
    expect(replace).toBe('?range=1h&mode=query&q=search+%22boom%22')
  })
})
