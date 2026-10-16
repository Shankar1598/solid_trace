import { describe, expect, it } from 'vitest'
import {
  explorerHref, formOfViz, movedPlaces, newChartQuery, rangeOfSearch, searchOfRange, titleOfQuery, vizOfForm, withSource,
} from './dashboard'

describe('a Dashboard\'s range', () => {
  it('is the URL\'s, relative or absolute, else the default range', () => {
    expect(rangeOfSearch('?range=6h', '24h')).toEqual({ kind: 'relative', span: '6h' })
    const absolute = rangeOfSearch('?from=2026-10-03T09:00:00.000Z&to=2026-10-03T10:00:00.000Z', '24h')
    expect(absolute).toEqual({ kind: 'absolute', from: new Date('2026-10-03T09:00:00Z'), to: new Date('2026-10-03T10:00:00Z') })
    expect(rangeOfSearch('', '24h')).toEqual({ kind: 'relative', span: '24h' })
    expect(rangeOfSearch('?other=1', 'nonsense')).toEqual({ kind: 'relative', span: '1h' })
  })

  it('round-trips through the URL', () => {
    const range = { kind: 'absolute' as const, from: new Date('2026-10-03T09:00:00Z'), to: new Date('2026-10-03T10:00:00Z') }
    expect(rangeOfSearch(searchOfRange(range), '1h')).toEqual(range)
    expect(searchOfRange({ kind: 'relative', span: '7d' })).toBe('?range=7d')
  })
})

describe('a chart', () => {
  it('maps its viz to the explorer\'s forms and back', () => {
    expect(formOfViz('bar')).toBe('bars')
    expect(formOfViz('stat')).toBe('stat')
    expect(vizOfForm('bars')).toBe('bar')
    expect(vizOfForm('table')).toBe('table')
  })

  it('opens in the explorer over its Project, Query and the range shown', () => {
    const href = explorerHref('acme', 'shop', { query: "['shop']\n| summarize count() by service", viz: 'bar' }, { kind: 'relative', span: '6h' })
    const url = new URL(href, 'http://x')
    expect(url.pathname).toBe('/acme/projects/shop/logs')
    expect(url.searchParams.get('range')).toBe('6h')
    expect(url.searchParams.get('q')).toBe("['shop']\n| summarize count() by service")
    expect(url.searchParams.get('viz')).toBe('bars')
    expect(url.searchParams.get('mode')).toBe('builder')
  })

  it('reads another Project by renaming its Query\'s source', () => {
    expect(withSource("['shop']\n| summarize count()", 'billing')).toBe("['billing']\n| summarize count()")
    expect(withSource("  [ 'it\\'s' ] | take 5", 'api')).toBe("['api'] | take 5")
    expect(withSource('summarize count()', 'api')).toBe('summarize count()')
    expect(newChartQuery('api')).toBe("['api']\n| summarize count() by bin_auto(time)")
  })

  it('takes its title from its Query\'s last stage', () => {
    expect(titleOfQuery("['shop']\n| where level == \"error\"\n| summarize count() by bin_auto(time)")).toBe('summarize count() by bin_auto(time)')
    expect(titleOfQuery("['shop']")).toBe('Logs')
    expect(titleOfQuery(`summarize ${'x'.repeat(100)}`)).toHaveLength(80)
  })
})

describe('a layout change', () => {
  it('moves only the charts whose place changed', () => {
    const charts = [{ id: 1, x: 0, y: 0, w: 6, h: 4 }, { id: 2, x: 6, y: 0, w: 6, h: 4 }]
    const layout = [{ i: '1', x: 0, y: 0, w: 6, h: 4 }, { i: '2', x: 0, y: 4, w: 12, h: 4 }, { i: '9', x: 0, y: 0, w: 1, h: 1 }]
    expect(movedPlaces(charts, layout)).toEqual([{ id: 2, x: 0, y: 4, w: 12, h: 4 }])
  })
})
