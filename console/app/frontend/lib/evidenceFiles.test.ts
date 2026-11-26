import { describe, expect, it } from 'vitest'
import { evidenceTitle, evidenceBytesPath, evidencePath, readSpeed, saveSpeed, type EvidenceFile } from './evidenceFiles'
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

const file = (content_type: string, caption: string | null = null): EvidenceFile => ({
  id: 'f1', caption, content_type, byte_size: 10, agent_session_id: 's1', uploaded_by: 'Ada', created_at: '2026-11-26T10:00:00Z', deleted_at: null,
})

describe('the video speed', () => {
  it('is 1x before the viewer picks one', () => {
    expect(readSpeed(memoryStorage())).toBe('1')
  })

  it('is the viewer\'s last pick', () => {
    const storage = memoryStorage()
    saveSpeed(storage, '0.5')
    expect(readSpeed(storage)).toBe('0.5')
    saveSpeed(storage, '2')
    expect(readSpeed(storage)).toBe('2')
  })

  it('is 1x for a kept value that is not one of the speeds', () => {
    const storage = memoryStorage()
    storage.setItem('solidtrace.evidenceVideo.speed', '16')
    expect(readSpeed(storage)).toBe('1')
  })

  it('works with no storage, or a storage that refuses', () => {
    expect(readSpeed(null)).toBe('1')
    expect(readSpeed(blocked)).toBe('1')
    expect(() => saveSpeed(blocked, '2')).not.toThrow()
    expect(() => saveSpeed(null, '2')).not.toThrow()
  })
})

describe('an Evidence file', () => {
  it('has its page and its bytes under the Organization', () => {
    expect(evidencePath('acme', 'f1')).toBe('/acme/evidence/f1')
    expect(evidenceBytesPath('acme', 'f1')).toBe('/acme/evidence/f1/file')
  })

  it('is named by its kind and its caption, as the PR body names it', () => {
    expect(evidenceTitle(file('image/png', 'Cart page'))).toBe('Screenshot: Cart page')
    expect(evidenceTitle(file('video/webm', 'Checkout'))).toBe('Video: Checkout')
    expect(evidenceTitle(file('image/png'))).toBe('Screenshot')
    expect(evidenceTitle(file('video/webm'))).toBe('Video')
  })
})
