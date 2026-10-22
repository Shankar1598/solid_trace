import { describe, expect, it } from 'vitest'
import {
  chosenText, explorerHref, filterValues, filterValuesOfSearch, formOfViz, movedPlaces, newChartQuery, optionsOfText,
  parametersOfChart, rangeOfSearch, searchOfRange, textOfOptions, titleOfQuery, unknownFilters, vizOfForm, withSource,
  type DashboardFilter,
} from './dashboard'

const LEVEL: DashboardFilter = {
  name: 'level', label: 'Level', kind: 'select', multiple: true,
  options: { from: 'list', items: [{ label: 'Error', value: 'error' }, { label: 'Warn', value: 'warn' }, { label: 'Info', value: 'info' }] },
}
const ENV: DashboardFilter = { name: 'env', label: 'env', kind: 'select', multiple: false, options: { from: 'list', items: [{ label: 'prod', value: 'prod' }] } }
const Q: DashboardFilter = { name: 'q', label: 'Search', kind: 'search' }
const FILTERS = [LEVEL, ENV, Q]

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

describe('a Dashboard\'s filter values', () => {
  it('are read from v_ keys beside the range, a repeated key as several values', () => {
    const values = filterValuesOfSearch('?range=1h&v_q=time+out&v_level=error&v_level=warn&v_nope=x', FILTERS)
    expect(values).toEqual({ level: ['error', 'warn'], q: ['time out'] })
  })

  it('leave out All, and keep one value for a filter that takes one', () => {
    expect(filterValuesOfSearch('?v_level=&v_env=prod&v_env=dev&v_q=', FILTERS)).toEqual({ env: ['prod'] })
    expect(filterValues(FILTERS, { q: ['x'], env: [], level: ['info'], gone: ['y'] })).toEqual({ level: ['info'], q: ['x'] })
  })

  it('are written beside the range, and round-trip through the URL', () => {
    const search = searchOfRange({ kind: 'relative', span: '6h' }, { level: ['error', 'warn'], q: ['time out'] })
    expect(search).toBe('?range=6h&v_level=error&v_level=warn&v_q=time+out')
    expect(rangeOfSearch(search, '1h')).toEqual({ kind: 'relative', span: '6h' })
    expect(filterValuesOfSearch(search, FILTERS)).toEqual({ level: ['error', 'warn'], q: ['time out'] })
  })

  it('a chart sends are those of the Dashboard\'s filters its Query names', () => {
    const values = { level: ['error'], env: ['prod'], q: ['x'] }
    expect(parametersOfChart('where level == $level\n| where message == "$q"', FILTERS, values)).toEqual({ level: ['error'] })
    expect(parametersOfChart('where env == $env and service == $nope | search $q', FILTERS, values)).toEqual({ env: ['prod'], q: ['x'] })
    expect(parametersOfChart('where env == $env', FILTERS, {})).toEqual({})
    expect(parametersOfChart('summarize count()', FILTERS, values)).toEqual({})
  })

  it('a chart\'s Query may name filters the Dashboard lacks, which run as All', () => {
    expect(unknownFilters('where level == $level and env == $nope | search $q2', FILTERS)).toEqual(['nope', 'q2'])
    expect(unknownFilters('where level == $level', FILTERS)).toEqual([])
  })

  it('read in the bar as their labels, up to two, else a count', () => {
    expect(chosenText(LEVEL, [])).toBe('All')
    expect(chosenText(LEVEL, ['error'])).toBe('Error')
    expect(chosenText(LEVEL, ['error', 'gone'])).toBe('Error, gone')
    expect(chosenText(LEVEL, ['error', 'warn', 'info'])).toBe('3 values')
    expect(chosenText(Q, ['time out'])).toBe('time out')
  })
})

describe('a select\'s options typed as text', () => {
  it('are one per line, a value or Label = value', () => {
    expect(optionsOfText('error\n Warning = warn \n\ninfo\n')).toEqual([
      { label: 'error', value: 'error' }, { label: 'Warning', value: 'warn' }, { label: 'info', value: 'info' },
    ])
  })

  it('round-trip, a label that is its value written once', () => {
    const items = [{ label: 'error', value: 'error' }, { label: 'Warning', value: 'warn' }]
    expect(textOfOptions(items)).toBe('error\nWarning = warn')
    expect(optionsOfText(textOfOptions(items))).toEqual(items)
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
