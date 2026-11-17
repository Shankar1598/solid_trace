import { describe, expect, it } from 'vitest'
import type { StorageLike } from '../recentQueries'
import { readFolded, writeFolded } from './foldedSections'

function memory(): StorageLike & { items: Map<string, string> } {
  const items = new Map<string, string>()
  return { items, getItem: k => items.get(k) ?? null, setItem: (k, v) => void items.set(k, v), removeItem: k => void items.delete(k) }
}

const broken: StorageLike = {
  getItem: () => { throw new Error('blocked') },
  setItem: () => { throw new Error('full') },
  removeItem: () => { throw new Error('blocked') },
}

describe('folded Sections', () => {
  it('are kept per Dashboard', () => {
    const storage = memory()
    writeFolded(storage, 1, new Set([3, 5]))
    writeFolded(storage, 2, new Set([7]))

    expect(readFolded(storage, 1)).toEqual(new Set([3, 5]))
    expect(readFolded(storage, 2)).toEqual(new Set([7]))
    expect(readFolded(storage, 9)).toEqual(new Set())
  })

  it('remove the key when none is folded', () => {
    const storage = memory()
    writeFolded(storage, 1, new Set([3]))
    writeFolded(storage, 1, new Set())

    expect(storage.items.size).toBe(0)
  })

  it('are none where the storage is missing, broken or holds something else', () => {
    expect(readFolded(null, 1)).toEqual(new Set())
    expect(readFolded(broken, 1)).toEqual(new Set())
    expect(() => writeFolded(broken, 1, new Set([3]))).not.toThrow()

    const storage = memory()
    storage.setItem('solidtrace.foldedSections.1', '{"not": "a list"}')
    expect(readFolded(storage, 1)).toEqual(new Set())
    storage.setItem('solidtrace.foldedSections.1', '[3, "x", 4.5, 6]')
    expect(readFolded(storage, 1)).toEqual(new Set([3, 6]))
    storage.setItem('solidtrace.foldedSections.1', 'not json')
    expect(readFolded(storage, 1)).toEqual(new Set())
  })
})
