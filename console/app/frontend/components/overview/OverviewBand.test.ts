import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import type { Overview } from '@/lib/overview'
import { OverviewBand, bandSteps } from './OverviewBand'

type Band = NonNullable<Overview['band']>

const band = (dropped: Partial<Pick<Band, 'unreadable_logs' | 'filtered_logs' | 'dropped_by_transform'>>): Band => ({
  received: { logs: 1000, bytes: 100_000, bytes_on_wire: 20_000, requests: 10 },
  kept: { logs: 600, bytes: 60_000 },
  stored: { logs: 600, bytes: 6_000, local_bytes: 6_000, bucket_bytes: 0 },
  ratio: 10,
  unreadable_logs: 0,
  filtered_logs: 0,
  dropped_by_transform: 0,
  overloaded_requests: 0,
  ...dropped,
})

const keptSlices = (b: Band) => bandSteps(b).find(step => step.key === 'kept')!.slices!

const html = (b: Band) => renderToStaticMarkup(createElement(OverviewBand, { band: b }))

describe('the Overview band', () => {
  it('shows the Logs filtered out as not of a Captured repository after Kept, as wide as their share of Received', () => {
    expect(keptSlices(band({ filtered_logs: 300, dropped_by_transform: 100 }))).toEqual([
      expect.objectContaining({ key: 'filtered', label: '300 Logs not of a Captured repository', bytes: 30_000 }),
      expect.objectContaining({ key: 'dropped_by_transform', label: '100 Logs dropped by the Log transform', bytes: 10_000 }),
    ])
  })

  it('puts the slices in the order the Logs are dropped: unreadable, filtered, then by the Log transform', () => {
    const slices = keptSlices(band({ unreadable_logs: 50, filtered_logs: 300, dropped_by_transform: 50 }))

    expect(slices.map(slice => slice.key)).toEqual(['unreadable', 'filtered', 'dropped_by_transform'])
  })

  it('names the filtered Logs on their slice and in the facts below the band', () => {
    const shown = html(band({ filtered_logs: 300 }))

    expect(shown).toContain('aria-label="Dropped: 300 Logs not of a Captured repository"')
    expect(shown).toMatch(/Filtered out as not of a Captured repository<\/dt><dd[^>]*>300 Logs<\/dd>/)
  })

  it('has no filtered slice or fact when nothing was filtered out', () => {
    const shown = html(band({ unreadable_logs: 50 }))

    expect(keptSlices(band({ unreadable_logs: 50 })).map(slice => slice.key)).toEqual(['unreadable'])
    expect(shown).not.toContain('Captured repository')
    expect(shown).toContain('aria-label="Dropped: 50 unreadable Logs"')
  })
})
