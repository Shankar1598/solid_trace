import { describe, expect, it } from 'vitest'
import { shortUserAgent } from './userAgent'

describe('shortUserAgent', () => {
  it('names the browser and the OS', () => {
    expect(shortUserAgent('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36'))
      .toBe('Chrome on macOS')
    expect(shortUserAgent('Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0')).toBe('Firefox on Linux')
    expect(shortUserAgent('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0'))
      .toBe('Edge on Windows')
    expect(shortUserAgent('Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1'))
      .toBe('Safari on iOS')
  })

  it('says Browser for an agent it does not know, and leaves out an OS it does not know', () => {
    expect(shortUserAgent('curl/8.5.0')).toBe('Browser')
    expect(shortUserAgent('')).toBe('Browser')
  })
})
