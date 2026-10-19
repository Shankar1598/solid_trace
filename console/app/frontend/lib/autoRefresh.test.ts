import { describe, expect, it } from 'vitest'
import { readLogsAutoRefresh, saveLogsAutoRefresh } from './autoRefresh'
import type { StorageLike } from './recentQueries'

function memory(): StorageLike {
  const items = new Map<string, string>()
  return { getItem: k => items.get(k) ?? null, setItem: (k, v) => void items.set(k, v), removeItem: k => void items.delete(k) }
}

const broken: StorageLike = {
  getItem: () => { throw new Error('SecurityError') },
  setItem: () => { throw new Error('QuotaExceededError') },
  removeItem: () => { throw new Error('SecurityError') },
}

describe("the explorer's auto-refresh", () => {
  it('is 15s until one is picked', () => {
    expect(readLogsAutoRefresh(memory())).toBe('15s')
    expect(readLogsAutoRefresh(null)).toBe('15s')
  })

  it('keeps the one picked, off too', () => {
    const storage = memory()
    saveLogsAutoRefresh(storage, '1m')
    expect(readLogsAutoRefresh(storage)).toBe('1m')
    saveLogsAutoRefresh(storage, 'off')
    expect(readLogsAutoRefresh(storage)).toBe('off')
  })

  it('reads anything else as 15s', () => {
    const storage = memory()
    storage.setItem('solidtrace.logsAutoRefresh', '10s')
    expect(readLogsAutoRefresh(storage)).toBe('15s')
  })

  it('stays 15s where storage throws', () => {
    expect(() => saveLogsAutoRefresh(broken, '5m')).not.toThrow()
    expect(readLogsAutoRefresh(broken)).toBe('15s')
  })
})
