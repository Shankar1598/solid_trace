import { describe, expect, it } from 'vitest'
import { limitStatus, untilLimitText, type OverviewStorage } from './overviewStorage'

const GIB = 2 ** 30

function storage(over: Partial<OverviewStorage>): OverviewStorage {
  return {
    whole_install: true, local_bytes: 45 * GIB, disk_limit_bytes: 50 * GIB, limit_share: 0.9,
    disk_bytes: 100 * GIB, free_bytes: 50 * GIB, bucket: false, bucket_bytes: 0,
    growth_bytes_per_day: GIB, projected_local_bytes: 48 * GIB, stays_under_limit: true, days_until_limit: null,
    old_files: null, ...over,
  }
}

describe('limitStatus', () => {
  it('is within the limit at 90% when steady state stays under it, as there is no day count', () => {
    expect(limitStatus(storage({}))).toBe('ok')
  })

  it('is near the limit with a day count under 14 days, or at 80% with one', () => {
    expect(limitStatus(storage({ stays_under_limit: false, days_until_limit: 5, limit_share: 0.5 }))).toBe('warning')
    expect(limitStatus(storage({ stays_under_limit: false, days_until_limit: 90, limit_share: 0.85 }))).toBe('warning')
    expect(limitStatus(storage({ stays_under_limit: false, days_until_limit: 90, limit_share: 0.5 }))).toBe('ok')
  })

  it('is past the limit past it', () => {
    expect(limitStatus(storage({ limit_share: 1.1, local_bytes: 55 * GIB, stays_under_limit: false, days_until_limit: 0 }))).toBe('error')
  })
})

describe('untilLimitText', () => {
  it('says the disk stays under the limit, at its steady-state size, with no day count', () => {
    expect(untilLimitText(storage({}))).toBe('Stays under the limit: about 48 GiB at steady state, as Retention deletes about as much as arrives.')
  })

  it('counts the days when steady state is past the limit', () => {
    expect(untilLimitText(storage({ stays_under_limit: false, days_until_limit: 5, projected_local_bytes: 80 * GIB })))
      .toBe('About 5 days until the limit at this growth; steady state would be about 80 GiB.')
  })
})
