import { describe, expect, it } from 'vitest'
import {
  chooseViz,
  fillEmptyQuery,
  openExplorer,
  pickRange,
  resolveRange,
  runExplorer,
  runsOnOpen,
  searchOf,
  stateFromSearch,
  switchMode,
  typeQuery,
  typeSql,
  type Explorer,
} from './explorerState'

describe('the URL of an explorer state', () => {
  it('opens on the last hour in the Builder with no Query', () => {
    expect(stateFromSearch('')).toEqual({ range: { kind: 'relative', span: '1h' }, mode: 'builder', query: '', sql: '', viz: 'auto' })
  })

  it('keeps a relative range relative', () => {
    const state = stateFromSearch('?range=15m&mode=builder')

    expect(state.range).toEqual({ kind: 'relative', span: '15m' })
    expect(searchOf(state)).toBe('?range=15m&mode=builder')
  })

  it('writes an absolute range as UTC ISO times', () => {
    const state = stateFromSearch('?from=2026-10-03T11:00:00%2B02:00&to=2026-10-03T10:30:00.250Z')

    expect(searchOf(state)).toBe(
      '?from=2026-10-03T09%3A00%3A00.000Z&to=2026-10-03T10%3A30%3A00.250Z&mode=builder',
    )
  })

  it('reads back the mode and Query text it wrote', () => {
    const state = { range: { kind: 'relative', span: '7d' }, mode: 'query', query: 'where service == "api"\n| search "a&b"', sql: '', viz: 'auto' } as const

    expect(stateFromSearch(searchOf(state))).toEqual(state)
  })

  it('reads back the SQL text it wrote', () => {
    const state = {
      range: { kind: 'relative', span: '1h' }, mode: 'sql', query: 'take 5', sql: "SELECT *\nFROM logs\nWHERE message LIKE '%a&b%'", viz: 'auto',
    } as const

    expect(searchOf(state)).toContain('sql=')
    expect(stateFromSearch(searchOf(state))).toEqual(state)
  })

  it('opens a link in SQL mode in the Builder, without its SQL, while SQL queries are off', () => {
    const search = '?range=1h&mode=sql&q=take+5&sql=SELECT+1'

    expect(stateFromSearch(search, ['builder', 'query'])).toEqual({
      range: { kind: 'relative', span: '1h' }, mode: 'builder', query: 'take 5', sql: '', viz: 'auto',
    })
  })

  it('reads back an absolute range it wrote', () => {
    const state = stateFromSearch('?from=2026-10-01T00:00:00.000Z&to=2026-10-02T00:00:00.000Z&mode=builder')

    expect(stateFromSearch(searchOf(state))).toEqual(state)
  })

  it('falls back to the defaults for a range or mode it cannot read', () => {
    expect(stateFromSearch('?range=yesterday&mode=sideways')).toEqual(stateFromSearch(''))
    expect(stateFromSearch('?from=2026-10-02T00:00:00Z&to=2026-10-01T00:00:00Z').range)
      .toEqual({ kind: 'relative', span: '1h' })
    expect(stateFromSearch('?from=2026-10-02T00:00:00Z').range).toEqual({ kind: 'relative', span: '1h' })
  })
})

describe('the chart a table is drawn as', () => {
  it('follows the table\'s shape when the URL has none, and writes none', () => {
    const state = stateFromSearch('?range=1h&mode=builder&q=take+5')

    expect(state.viz).toBe('auto')
    expect(searchOf(state)).toBe('?range=1h&mode=builder&q=take+5')
  })

  it('reads back each chart it wrote', () => {
    for (const viz of ['line', 'area', 'bars', 'table'] as const) {
      const state = { ...stateFromSearch('?range=24h&mode=query&q=summarize+count()'), viz }

      expect(searchOf(state)).toContain(`viz=${viz}`)
      expect(stateFromSearch(searchOf(state))).toEqual(state)
    }
  })

  it('ignores a chart it cannot read', () => {
    expect(stateFromSearch('?range=1h&mode=builder&viz=pie').viz).toBe('auto')
    expect(stateFromSearch('?range=1h&mode=builder&viz=').viz).toBe('auto')
    expect(stateFromSearch('?range=1h&mode=builder&viz=Line').viz).toBe('auto')
  })
})

describe('opening a link', () => {
  it('runs its Query in the Builder and Query modes', () => {
    expect(runsOnOpen(openExplorer('?range=1h&mode=builder&q=search+%22boom%22'))).toBe(true)
    expect(runsOnOpen(openExplorer('?range=1h&mode=query&q=take+5'))).toBe(true)
  })

  it('never runs its SQL', () => {
    const explorer = openExplorer('?range=1h&mode=sql&q=take+5&sql=DROP+TABLE+logs_late')

    expect(explorer.current.sql).toBe('DROP TABLE logs_late')
    expect(runsOnOpen(explorer)).toBe(false)
  })

  it('runs its Query when SQL queries are off, never the SQL', () => {
    const explorer = openExplorer('?range=1h&mode=sql&q=take+5&sql=DROP+TABLE+logs_late', ['builder', 'query'])

    expect(runsOnOpen(explorer)).toBe(true)
    expect(explorer.current).toMatchObject({ query: 'take 5', sql: '' })
  })
})

describe('resolving a time range', () => {
  const now = new Date('2026-10-03T10:00:00.000Z')

  it('ends a relative range now', () => {
    expect(resolveRange({ kind: 'relative', span: '15m' }, now)).toEqual({
      from: new Date('2026-10-03T09:45:00.000Z'),
      to: now,
    })
    expect(resolveRange({ kind: 'relative', span: '7d' }, now).from).toEqual(new Date('2026-09-26T10:00:00.000Z'))
  })

  it('keeps an absolute range as given', () => {
    const range = { kind: 'absolute', from: new Date('2026-10-01T00:00:00Z'), to: new Date('2026-10-02T00:00:00Z') } as const

    expect(resolveRange(range, now)).toEqual({ from: range.from, to: range.to })
  })
})

describe('history entries', () => {
  // A browser history: runs push entries, Back opens the previous one.
  function browser(search: string) {
    const entries = [search]
    let explorer = openExplorer(search)
    return {
      get explorer(): Explorer { return explorer },
      get entries() { return entries },
      type(text: string) { explorer = typeQuery(explorer, text) },
      typeSql(text: string) { explorer = typeSql(explorer, text) },
      pickRange(span: string) { explorer = pickRange(explorer, { kind: 'relative', span }) },
      chooseViz(viz: 'auto' | 'line' | 'area' | 'bars' | 'table') {
        const { explorer: next, replace } = chooseViz(explorer, viz)
        explorer = next
        entries[entries.length - 1] = replace
      },
      switchMode(mode: 'builder' | 'query' | 'sql') {
        const { explorer: next, replace } = switchMode(explorer, mode)
        explorer = next
        entries[entries.length - 1] = replace
      },
      run() {
        const { explorer: next, push } = runExplorer(explorer)
        explorer = next
        if (push) entries.push(push)
      },
      back() {
        entries.pop()
        explorer = openExplorer(entries[entries.length - 1])
      },
    }
  }

  it('adds none while the Query is typed', () => {
    const b = browser('?range=1h&mode=builder')

    for (const text of ['s', 'se', 'search', 'search "boom"']) b.type(text)

    expect(b.entries).toHaveLength(1)
    expect(b.explorer.current.query).toBe('')
    expect(b.explorer.draft.query).toBe('search "boom"')
  })

  it('adds one per run, holding what ran', () => {
    const b = browser('?range=1h&mode=builder')

    b.type('search "boom"')
    b.run()
    b.pickRange('24h')
    b.run()

    expect(b.entries).toEqual([
      '?range=1h&mode=builder',
      '?range=1h&mode=builder&q=search+%22boom%22',
      '?range=24h&mode=builder&q=search+%22boom%22',
    ])
  })

  it('lets Back undo the last run', () => {
    const b = browser('?range=1h&mode=builder')
    b.type('search "boom"')
    b.run()
    b.type('search "bang"')
    b.run()

    b.back()

    expect(b.explorer.current.query).toBe('search "boom"')
    expect(b.explorer.draft.query).toBe('search "boom"')
  })

  it('adds none when a run changes nothing', () => {
    const b = browser('?range=1h&mode=builder')

    b.run()

    expect(b.entries).toHaveLength(1)
  })

  it('adds none when the mode switches, but keeps the mode in the URL', () => {
    const b = browser('?range=1h&mode=builder&q=search+%22boom%22')
    b.type('search "bang"')

    b.switchMode('query')

    expect(b.entries).toEqual(['?range=1h&mode=query&q=search+%22boom%22'])
    expect(b.explorer.current).toMatchObject({ mode: 'query', query: 'search "boom"' })
    expect(b.explorer.draft).toMatchObject({ mode: 'query', query: 'search "bang"' })
  })

  it('drops a Query typed but never run on switching to the Builder, which shows what ran', () => {
    const b = browser('?range=24h&mode=query&q=search+%22boom%22')
    b.type('search "bang"')

    b.switchMode('builder')

    expect(b.explorer.draft).toMatchObject({ mode: 'builder', query: 'search "boom"' })
    // The next Run or range pick runs what the Builder shows.
    b.pickRange('1h')
    b.run()
    expect(b.entries.at(-1)).toBe('?range=1h&mode=builder&q=search+%22boom%22')
  })

  it('adds none when the chart changes, but keeps it in the URL without running', () => {
    const b = browser('?range=1h&mode=query&q=summarize+count()')
    b.type('summarize count() by service')

    b.chooseViz('bars')

    expect(b.entries).toEqual(['?range=1h&mode=query&q=summarize+count%28%29&viz=bars'])
    expect(b.explorer.current).toMatchObject({ query: 'summarize count()', viz: 'bars' })
    expect(b.explorer.draft).toMatchObject({ query: 'summarize count() by service', viz: 'bars' })

    // The next run keeps the chart; going back to the shape's own chart leaves no trace.
    b.run()
    expect(b.entries.at(-1)).toContain('viz=bars')
    b.chooseViz('auto')
    expect(b.entries.at(-1)).not.toContain('viz')
  })

  it('adds one per SQL run, holding the SQL that ran, and reopens it without running it', () => {
    const b = browser('?range=1h&mode=sql&q=take+5&sql=SELECT+1')
    b.typeSql('SELECT 2')
    expect(b.entries).toHaveLength(1)
    b.run()
    b.typeSql('SELECT 3')
    b.run()

    b.back()

    expect(b.explorer.current).toMatchObject({ mode: 'sql', query: 'take 5', sql: 'SELECT 2' })
    expect(runsOnOpen(b.explorer)).toBe(false)
  })

  it('shows an empty Query in Query mode as the Project\'s slug, which is no edit', () => {
    const project = "['my-api']"
    // A Query-mode link with no Query.
    const opened = fillEmptyQuery(openExplorer('?range=1h&mode=query'), project)

    expect(opened.draft.query).toBe(project)
    expect(runExplorer(opened).push).toBeNull()
    const builder = switchMode(opened, 'builder').explorer
    expect(builder.draft.query).toBe(builder.current.query)

    // The Builder's empty Query, switched to Query mode.
    const switched = fillEmptyQuery(switchMode(openExplorer('?range=1h&mode=builder'), 'query').explorer, project)
    expect(switched.current.query).toBe(project)
    expect(runExplorer(switched).push).toBeNull()
  })

  it('fills only an empty Query, and only in Query mode', () => {
    const project = "['my-api']"

    expect(fillEmptyQuery(openExplorer('?range=1h&mode=builder'), project).current.query).toBe('')
    expect(fillEmptyQuery(openExplorer('?range=1h&mode=query&q=take+5'), project).current.query).toBe('take 5')
  })

  it('reopens a run in the mode it was left in', () => {
    const b = browser('?range=1h&mode=builder')
    b.type('search "boom"')
    b.run()
    b.switchMode('query')
    b.type('search "boom" | count')
    b.run()

    b.back()

    expect(b.explorer.current).toMatchObject({ mode: 'query', query: 'search "boom"' })
  })
})
