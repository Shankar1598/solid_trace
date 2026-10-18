import { describe, expect, it } from 'vitest'
import { MAX_RECENT, latestQueries, readRecent, recordQuery, type StorageLike } from './recentQueries'

function memory(): StorageLike & { items: Map<string, string> } {
  const items = new Map<string, string>()
  return { items, getItem: k => items.get(k) ?? null, setItem: (k, v) => void items.set(k, v), removeItem: k => void items.delete(k) }
}

const broken: StorageLike = {
  getItem: () => { throw new Error('SecurityError') },
  setItem: () => { throw new Error('QuotaExceededError') },
  removeItem: () => { throw new Error('SecurityError') },
}

// The viewer the tests read and record as.
const ME = 7

const ran = (query: string, at: number) => ({ query, search: `?range=1h&mode=query&q=${encodeURIComponent(query)}`, at })

describe('recent Queries', () => {
  it('keeps each Project\'s, newest first', () => {
    const storage = memory()
    recordQuery(storage, ME, 'org', 'api', ran('a', 1))
    recordQuery(storage, ME, 'org', 'api', ran('b', 2))
    recordQuery(storage, ME, 'org', 'web', ran('c', 3))

    expect(readRecent(storage, ME, 'org', 'api').map(r => r.query)).toEqual(['b', 'a'])
    expect(readRecent(storage, ME, 'org', 'web').map(r => r.query)).toEqual(['c'])
  })

  it('keeps a Query run again once, as the newest, with its last search', () => {
    const storage = memory()
    recordQuery(storage, ME, 'org', 'api', ran('a', 1))
    recordQuery(storage, ME, 'org', 'api', ran('b', 2))
    recordQuery(storage, ME, 'org', 'api', { ...ran('a', 3), search: '?range=6h' })

    expect(readRecent(storage, ME, 'org', 'api')).toEqual([{ query: 'a', search: '?range=6h', at: 3 }, ran('b', 2)])
  })

  it(`keeps at most ${MAX_RECENT} per Project`, () => {
    const storage = memory()
    for (let i = 0; i < MAX_RECENT + 5; i++) recordQuery(storage, ME, 'org', 'api', ran(`q${i}`, i))

    const kept = readRecent(storage, ME, 'org', 'api')
    expect(kept).toHaveLength(MAX_RECENT)
    expect(kept[0].query).toBe(`q${MAX_RECENT + 4}`)
  })

  it('forgets quietly when storage is missing, throws or holds something else', () => {
    expect(() => recordQuery(broken, ME, 'org', 'api', ran('a', 1))).not.toThrow()
    expect(readRecent(broken, ME, 'org', 'api')).toEqual([])
    expect(readRecent(null, ME, 'org', 'api')).toEqual([])

    const storage = memory()
    recordQuery(storage, ME, 'org', 'api', ran('a', 1))
    const [key] = storage.items.keys()
    storage.items.set(key, '{not json')
    expect(readRecent(storage, ME, 'org', 'api')).toEqual([])
    storage.items.set(key, JSON.stringify([{ query: 'a' }, ran('b', 2), 'c']))
    expect(readRecent(storage, ME, 'org', 'api')).toEqual([ran('b', 2)])
  })

  it('keeps each viewer\'s own, so two people on one browser don\'t share them', () => {
    const storage = memory()
    recordQuery(storage, ME, 'org', 'api', ran('mine', 1))
    recordQuery(storage, 8, 'org', 'api', ran('theirs', 2))

    expect(readRecent(storage, ME, 'org', 'api').map(r => r.query)).toEqual(['mine'])
    expect(latestQueries(storage, 8, 'org', ['api']).map(r => r.query)).toEqual(['theirs'])
  })

  it('reads the list kept before viewers had their own once, then drops it', () => {
    const storage = memory()
    storage.items.set('solidtrace.recentQueries.org/api', JSON.stringify([ran('old', 1)]))

    expect(readRecent(storage, ME, 'org', 'api')).toEqual([ran('old', 1)])
    expect(storage.items.has('solidtrace.recentQueries.org/api')).toBe(false)
    expect(readRecent(storage, ME, 'org', 'api')).toEqual([ran('old', 1)])
    expect(readRecent(storage, 8, 'org', 'api')).toEqual([])
  })

  it('merges several Projects\' newest first, each saying its Project', () => {
    const storage = memory()
    recordQuery(storage, ME, 'org', 'api', ran('a', 1))
    recordQuery(storage, ME, 'org', 'web', ran('b', 2))
    recordQuery(storage, ME, 'org', 'api', ran('c', 3))

    expect(latestQueries(storage, ME, 'org', ['api', 'web'], 2)).toEqual([
      { project: 'api', ...ran('c', 3) },
      { project: 'web', ...ran('b', 2) },
    ])
  })
})
