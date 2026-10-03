import { describe, expect, it } from 'vitest'
import {
  openExplorer,
  pickRange,
  resolveRange,
  runExplorer,
  searchOf,
  stateFromSearch,
  typeQuery,
  type Explorer,
} from './explorerState'

describe('the URL of an explorer state', () => {
  it('opens on the last hour in the Builder with no Query', () => {
    expect(stateFromSearch('')).toEqual({ range: { kind: 'relative', span: '1h' }, mode: 'builder', query: '' })
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
    const state = { range: { kind: 'relative', span: '7d' }, mode: 'query', query: 'where service == "api"\n| search "a&b"' } as const

    expect(stateFromSearch(searchOf(state))).toEqual(state)
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
      pickRange(span: string) { explorer = pickRange(explorer, { kind: 'relative', span }) },
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
})
