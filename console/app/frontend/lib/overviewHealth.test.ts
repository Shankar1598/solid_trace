import { describe, expect, it } from 'vitest'
import { msText, waitText } from './overviewHealth'

describe("Health's times", () => {
  it('writes a read in ms under a second, then in seconds', () => {
    expect(msText(4.25)).toBe('4.25 ms')
    expect(msText(840)).toBe('840 ms')
    expect(msText(2500)).toBe('2.5 s')
  })

  it('writes a wait in seconds, minutes, then hours', () => {
    expect(waitText(45)).toBe('45 s')
    expect(waitText(120)).toBe('2 min')
    expect(waitText(125)).toBe('2 min 5 s')
    expect(waitText(3 * 3600 + 600)).toBe('3 h 10 min')
  })
})
