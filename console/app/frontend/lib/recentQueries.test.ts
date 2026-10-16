import { describe, expect, it } from 'vitest'
import { MAX_RECENT, latestQueries, readRecent, recordQuery, type StorageLike } from './recentQueries'

function memory(): StorageLike & { items: Map<string, string> } {
  const items = new Map<string, string>()
  return { items, getItem: k => items.get(k) ?? null, setItem: (k, v) => void items.set(k, v) }
}

const broken: StorageLike = {
  getItem: () => { throw new Error('SecurityError') },
  setItem: () => { throw new Error('QuotaExceededError') },
}

const ran = (query: string, at: number) => ({ query, search: `?range=1h&mode=query&q=${encodeURIComponent(query)}`, at })

describe('recent Queries', () => {
  it('keeps each Project\'s, newest first', () => {
    const storage = memory()
    recordQuery(storage, 'org', 'api', ran('a', 1))
    recordQuery(storage, 'org', 'api', ran('b', 2))
    recordQuery(storage, 'org', 'web', ran('c', 3))

    expect(readRecent(storage, 'org', 'api').map(r => r.query)).toEqual(['b', 'a'])
    expect(readRecent(storage, 'org', 'web').map(r => r.query)).toEqual(['c'])
  })

  it('keeps a Query run again once, as the newest, with its last search', () => {
    const storage = memory()
    recordQuery(storage, 'org', 'api', ran('a', 1))
    recordQuery(storage, 'org', 'api', ran('b', 2))
    recordQuery(storage, 'org', 'api', { ...ran('a', 3), search: '?range=6h' })

    expect(readRecent(storage, 'org', 'api')).toEqual([{ query: 'a', search: '?range=6h', at: 3 }, ran('b', 2)])
  })

  it(`keeps at most ${MAX_RECENT} per Project`, () => {
    const storage = memory()
    for (let i = 0; i < MAX_RECENT + 5; i++) recordQuery(storage, 'org', 'api', ran(`q${i}`, i))

    const kept = readRecent(storage, 'org', 'api')
    expect(kept).toHaveLength(MAX_RECENT)
    expect(kept[0].query).toBe(`q${MAX_RECENT + 4}`)
  })

  it('forgets quietly when storage is missing, throws or holds something else', () => {
    expect(() => recordQuery(broken, 'org', 'api', ran('a', 1))).not.toThrow()
    expect(readRecent(broken, 'org', 'api')).toEqual([])
    expect(readRecent(null, 'org', 'api')).toEqual([])

    const storage = memory()
    recordQuery(storage, 'org', 'api', ran('a', 1))
    const [key] = storage.items.keys()
    storage.items.set(key, '{not json')
    expect(readRecent(storage, 'org', 'api')).toEqual([])
    storage.items.set(key, JSON.stringify([{ query: 'a' }, ran('b', 2), 'c']))
    expect(readRecent(storage, 'org', 'api')).toEqual([ran('b', 2)])
  })

  it('merges several Projects\' newest first, each saying its Project', () => {
    const storage = memory()
    recordQuery(storage, 'org', 'api', ran('a', 1))
    recordQuery(storage, 'org', 'web', ran('b', 2))
    recordQuery(storage, 'org', 'api', ran('c', 3))

    expect(latestQueries(storage, 'org', ['api', 'web'], 2)).toEqual([
      { project: 'api', ...ran('c', 3) },
      { project: 'web', ...ran('b', 2) },
    ])
  })
})
