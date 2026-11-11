import { describe, expect, it } from 'vitest'
import { fallbackHint, requiredScheme } from './publicUrls'

describe('URL fallback hints', () => {
  it('names the env var a blank URL uses', () => {
    expect(fallbackHint({ url: 'https://console.example.com', source: 'CONSOLE_URL' }))
      .toBe('Blank uses CONSOLE_URL: https://console.example.com')
    expect(fallbackHint({ url: 'https://ingest.example.com:4443', source: 'PUBLIC_INGEST_URL' }))
      .toBe('Blank uses PUBLIC_INGEST_URL: https://ingest.example.com:4443')
  })

  it("says when a blank one uses the request's address, or Console's host at port 4000", () => {
    expect(fallbackHint({ url: 'http://10.0.0.5:3000', source: 'request' }))
      .toBe('Blank uses the address Console was reached at: http://10.0.0.5:3000')
    expect(fallbackHint({ url: 'http://10.0.0.5:4000', source: 'console_url' }))
      .toBe("Blank uses Console's host at port 4000: http://10.0.0.5:4000")
  })

  it('says when there is nothing to fall back to', () => {
    expect(fallbackHint(null)).toMatch(/without a URL/)
  })
})

describe('the scheme a Console URL needs', () => {
  it("follows the HTTPS row Console read at start", () => {
    expect(requiredScheme('On')).toBe('https')
    expect(requiredScheme('Off')).toBe('http')
    expect(requiredScheme(undefined)).toBeNull()
  })
})
