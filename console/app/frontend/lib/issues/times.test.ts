import { describe, expect, it } from 'vitest'
import { issueAge } from './times'

const now = new Date('2026-10-18T12:00:00Z')

describe('issueAge', () => {
  it("counts from the Issue's first Event, not from when it was stored", () => {
    const issue = { created_at: '2026-10-18T11:00:00Z', first_seen_at: '2026-10-13T12:00:00Z' }
    expect(issueAge(issue, now)).toBe('5 days')
  })

  it('drops "about"', () => {
    expect(issueAge({ first_seen_at: '2026-10-18T10:00:00Z' }, now)).toBe('2 hours')
  })
})
