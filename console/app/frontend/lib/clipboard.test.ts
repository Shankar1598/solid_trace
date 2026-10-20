import { describe, expect, it, vi } from 'vitest'
import { copyToClipboard } from './clipboard'

const link = 'http://console.example.com/invites/abc?org=acme'

describe('copyToClipboard', () => {
  it('writes through the clipboard when the browser gives one', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    const execCopy = vi.fn(() => true)

    expect(await copyToClipboard(link, { writeText, execCopy })).toBe(true)
    expect(writeText).toHaveBeenCalledWith(link)
    expect(execCopy).not.toHaveBeenCalled()
  })

  it('copies through a hidden textarea when the page has no clipboard, as over plain HTTP', async () => {
    const execCopy = vi.fn(() => true)

    expect(await copyToClipboard(link, { execCopy })).toBe(true)
    expect(execCopy).toHaveBeenCalledWith(link)
  })

  it('copies through a hidden textarea when the clipboard refuses', async () => {
    const writeText = vi.fn(() => Promise.reject(new Error('NotAllowedError')))
    const execCopy = vi.fn(() => true)

    expect(await copyToClipboard(link, { writeText, execCopy })).toBe(true)
    expect(execCopy).toHaveBeenCalledWith(link)
  })

  it('says it failed when the textarea does not copy either', async () => {
    expect(await copyToClipboard(link, { execCopy: () => false })).toBe(false)
  })

  it('says it failed when the copy command throws', async () => {
    const execCopy = () => { throw new Error('SecurityError') }

    expect(await copyToClipboard(link, { execCopy })).toBe(false)
  })
})
