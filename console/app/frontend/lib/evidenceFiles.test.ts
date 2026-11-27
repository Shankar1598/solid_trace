import { describe, expect, it } from 'vitest'
import { evidenceDeletedText, evidenceFileOfLink, evidenceTitle, evidenceBytesPath, evidencePath, isPlainClick, readSpeed, saveSpeed, type EvidenceFile } from './evidenceFiles'
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

describe('the sentence of a deleted file', () => {
  it('gives the date and the whole days from the upload to the delete', () => {
    expect(evidenceDeletedText('2026-08-28T10:00:00Z', '2026-11-26T12:00:00Z'))
      .toBe('This evidence was deleted on Nov 26, 2026 by Evidence Retention, 90 days after its upload.')
  })

  it('says 1 day for one day', () => {
    expect(evidenceDeletedText('2026-11-25T10:00:00Z', '2026-11-26T12:00:00Z'))
      .toBe('This evidence was deleted on Nov 26, 2026 by Evidence Retention, 1 day after its upload.')
  })

  it('gives the UTC day of the delete in each time zone', () => {
    expect(evidenceDeletedText('2026-11-20T23:00:00Z', '2026-11-26T23:30:00Z')).toContain('deleted on Nov 26, 2026 ')
    expect(evidenceDeletedText('2026-11-20T00:00:00Z', '2026-11-26T00:10:00Z')).toContain('deleted on Nov 26, 2026 ')
  })
})

describe('a click on an Evidence file', () => {
  const click = { button: 0, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, defaultPrevented: false }

  it('opens the popup when it is a plain click', () => {
    expect(isPlainClick(click)).toBe(true)
  })

  it('opens the page in the browser\'s way with Cmd, Ctrl, Shift, Alt or another button', () => {
    for (const other of [{ metaKey: true }, { ctrlKey: true }, { shiftKey: true }, { altKey: true }, { button: 1 }, { defaultPrevented: true }]) {
      expect(isPlainClick({ ...click, ...other })).toBe(false)
    }
  })
})

describe('the Evidence file of a link', () => {
  const id = '7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f'

  it('is the file of a link to its page, as solidtrace evidence add prints it, on any host', () => {
    expect(evidenceFileOfLink(`https://console.example.com/acme/evidence/${id}`, 'acme', [id])).toBe(id)
    expect(evidenceFileOfLink(`/acme/evidence/${id}/`, 'acme', [id])).toBe(id)
  })

  it('is none for a file that is not in the list, of another Organization, or another page of the file', () => {
    expect(evidenceFileOfLink('https://console.example.com/acme/evidence/00000000-0000-4000-8000-000000000000', 'acme', [id])).toBeNull()
    expect(evidenceFileOfLink(`https://console.example.com/other/evidence/${id}`, 'acme', [id])).toBeNull()
    expect(evidenceFileOfLink(`https://console.example.com/acme/evidence/${id}/file`, 'acme', [id])).toBeNull()
  })

  it('is none for a link that is not a web address, or no link', () => {
    expect(evidenceFileOfLink(`javascript:alert(1)//acme/evidence/${id}`, 'acme', [id])).toBeNull()
    expect(evidenceFileOfLink(undefined, 'acme', [id])).toBeNull()
  })
})
