import { describe, expect, it } from 'vitest'
import { groupOpen, saveGroupOpen } from './sidebarGroups'
import type { StorageLike } from './recentQueries'

function memoryStorage(): StorageLike {
  const items = new Map<string, string>()
  return {
    getItem: k => items.get(k) ?? null,
    setItem: (k, v) => { items.set(k, v) },
    removeItem: k => { items.delete(k) },
  }
}

const blocked: StorageLike = {
  getItem: () => { throw new Error('blocked') },
  setItem: () => { throw new Error('blocked') },
  removeItem: () => { throw new Error('blocked') },
}

describe('a sidebar group', () => {
  it('is closed on another page before the viewer opens it', () => {
    expect(groupOpen(memoryStorage(), 'coding_agents', false)).toBe(false)
  })

  it('is open on the page of one of its children, whatever the viewer last chose', () => {
    const storage = memoryStorage()
    expect(groupOpen(storage, 'coding_agents', true)).toBe(true)
    saveGroupOpen(storage, 'coding_agents', false)
    expect(groupOpen(storage, 'coding_agents', true)).toBe(true)
  })

  it('stays as the viewer last left it on the other pages', () => {
    const storage = memoryStorage()
    saveGroupOpen(storage, 'coding_agents', true)
    expect(groupOpen(storage, 'coding_agents', false)).toBe(true)
    saveGroupOpen(storage, 'coding_agents', false)
    expect(groupOpen(storage, 'coding_agents', false)).toBe(false)
  })

  it('works with no storage, or a storage that refuses, open only on its children\'s pages', () => {
    for (const storage of [null, blocked]) {
      expect(() => saveGroupOpen(storage, 'coding_agents', true)).not.toThrow()
      expect(groupOpen(storage, 'coding_agents', false)).toBe(false)
      expect(groupOpen(storage, 'coding_agents', true)).toBe(true)
    }
  })
})
