import { describe, expect, it } from 'vitest'
import type { LogTable, TableColumn } from '@/lib/logs/table'
import { MAX_BARS, MAX_SERIES, chartOf, formFor, formsOf, type ChartModel } from './series'

const TIME: TableColumn = { name: 'time', type: 'datetime', bin_seconds: 300 }
const SERVICE: TableColumn = { name: 'service', type: 'string' }
const COUNT: TableColumn = { name: 'count_', type: 'number', agg: 'count' }
const AVG: TableColumn = { name: 'avg_duration_ms', type: 'number', agg: 'avg' }

const at = (time: string) => new Date(time).getTime()

function table(columns: TableColumn[], rows: unknown[][], extra: Partial<LogTable> = {}): LogTable {
  return { columns, rows, truncated: false, ...extra }
}

/** The model's series and points, for a time series or bars. */
function plot(model: ChartModel) {
  if (model.kind !== 'time' && model.kind !== 'bars') throw new Error(`expected a plot, got ${model.kind}`)
  return model
}

describe('a time series', () => {
  it('draws each number column over the bin column, oldest first', () => {
    const model = plot(chartOf(table([TIME, COUNT, AVG], [
      ['2026-10-09T10:05:00Z', 4, 12.5],
      ['2026-10-09T10:00:00Z', 3, 10],
    ])))

    expect(model.kind).toBe('time')
    expect(model.series.map(s => s.name)).toEqual(['count_', 'avg_duration_ms'])
    expect(model.points).toEqual([
      { x: at('2026-10-09T10:00:00Z'), s0: 3, s1: 10 },
      { x: at('2026-10-09T10:05:00Z'), s0: 4, s1: 12.5 },
    ])
  })

  it('pivots long rows to one series per value of a string column', () => {
    const model = plot(chartOf(table([TIME, SERVICE, COUNT], [
      ['2026-10-09T10:00:00Z', 'api', 12],
      ['2026-10-09T10:00:00Z', 'web', 3],
      ['2026-10-09T10:05:00Z', 'api', 9],
      ['2026-10-09T10:05:00Z', 'web', 5],
    ])))

    expect(model.series.map(s => ({ name: s.name, split: s.split }))).toEqual([
      { name: 'api', split: [{ field: 'service', value: 'api' }] },
      { name: 'web', split: [{ field: 'service', value: 'web' }] },
    ])
    expect(model.points).toEqual([
      { x: at('2026-10-09T10:00:00Z'), s0: 12, s1: 3 },
      { x: at('2026-10-09T10:05:00Z'), s0: 9, s1: 5 },
    ])
  })

  it('names a series by its split values and, with several number columns, its column', () => {
    const ok: TableColumn = { name: 'ok', type: 'bool' }
    const model = plot(chartOf(table([TIME, SERVICE, ok, COUNT, AVG], [
      ['2026-10-09T10:00:00Z', 'api', true, 12, 40],
      ['2026-10-09T10:00:00Z', null, false, 1, 9],
    ])))

    // Split values by the total of the first number column, then each number column in the table's order.
    expect(model.series.map(s => s.name)).toEqual([
      'api · true · count_', 'api · true · avg_duration_ms', 'no service · false · count_', 'no service · false · avg_duration_ms',
    ])
    expect(model.series.find(s => s.name === 'no service · false · count_')?.split).toEqual([
      { field: 'service', value: null }, { field: 'ok', value: false },
    ])
  })

  it('splits by a number column that comes before the aggregates, as a by column does', () => {
    const status: TableColumn = { name: 'status', type: 'number' }
    const model = plot(chartOf(table([TIME, status, COUNT], [
      ['2026-10-09T10:00:00Z', 200, 12],
      ['2026-10-09T10:00:00Z', 500, 2],
    ])))

    expect(model.series.map(s => s.name)).toEqual(['200', '500'])
  })

  it('draws a number column after the aggregates, such as arithmetic over them', () => {
    const rate: TableColumn = { name: 'per_minute', type: 'number' }
    const model = plot(chartOf(table([TIME, COUNT, rate], [['2026-10-09T10:00:00Z', 10, 2]])))

    expect(model.series.map(s => s.name)).toEqual(['count_', 'per_minute'])
  })

  it('takes its range from the table, else from its bins', () => {
    const rows = [['2026-10-09T10:00:00Z', 3], ['2026-10-09T10:10:00Z', 4]]
    const ranged = chartOf(table([TIME, COUNT], rows, { from: '2026-10-09T09:58:00Z', to: '2026-10-09T10:12:00Z' }))
    const bare = chartOf(table([TIME, COUNT], rows))

    expect(ranged).toMatchObject({ kind: 'time', binSeconds: 300, from: new Date('2026-10-09T09:58:00Z'), to: new Date('2026-10-09T10:12:00Z') })
    expect(bare).toMatchObject({ from: new Date('2026-10-09T10:00:00Z'), to: new Date('2026-10-09T10:15:00Z') })
  })
})

describe('filling empty bins', () => {
  const range = { from: '2026-10-09T09:58:00Z', to: '2026-10-09T10:15:00Z' }

  it('fills the bins from the range\'s first to its last with zero counts', () => {
    const model = plot(chartOf(table([TIME, COUNT], [['2026-10-09T10:05:00Z', 4]], { fill_gaps: true, ...range })))

    // Bins start on whole multiples of their size, so the first starts before from.
    expect(model.points).toEqual([
      { x: at('2026-10-09T09:55:00Z'), s0: 0 },
      { x: at('2026-10-09T10:00:00Z'), s0: 0 },
      { x: at('2026-10-09T10:05:00Z'), s0: 4 },
      { x: at('2026-10-09T10:10:00Z'), s0: 0 },
    ])
  })

  it('leaves a gap for an aggregate that has no value in an empty bin', () => {
    const columns: TableColumn[] = [
      TIME, { name: 'n', type: 'number', agg: 'countif' }, { name: 'd', type: 'number', agg: 'dcount' }, { name: 's', type: 'number', agg: 'sum' },
      AVG, { name: 'lo', type: 'number', agg: 'min' }, { name: 'hi', type: 'number', agg: 'max' }, { name: 'p', type: 'number', agg: 'percentile' },
    ]
    const model = plot(chartOf(table(columns, [['2026-10-09T10:05:00Z', 1, 1, 1, 1, 1, 1, 1]], { fill_gaps: true, ...range })))

    expect(model.points[1]).toEqual({ x: at('2026-10-09T10:00:00Z'), s0: 0, s1: 0, s2: 0, s3: null, s4: null, s5: null, s6: null })
  })

  it('fills a series missing from a bin the same way', () => {
    const model = plot(chartOf(table([TIME, SERVICE, COUNT], [
      ['2026-10-09T10:00:00Z', 'api', 12],
      ['2026-10-09T10:00:00Z', 'web', 3],
      ['2026-10-09T10:05:00Z', 'api', 9],
    ], { fill_gaps: true, ...range })))

    expect(model.points.find(p => p.x === at('2026-10-09T10:05:00Z'))).toEqual({ x: at('2026-10-09T10:05:00Z'), s0: 9, s1: 0 })
  })

  it('fills nothing when the table says a missing bin may have Logs', () => {
    const model = plot(chartOf(table([TIME, SERVICE, COUNT], [
      ['2026-10-09T10:00:00Z', 'api', 12],
      ['2026-10-09T10:00:00Z', 'web', 3],
      ['2026-10-09T10:10:00Z', 'api', 9],
    ], { fill_gaps: false, ...range })))

    expect(model.points).toEqual([
      { x: at('2026-10-09T10:00:00Z'), s0: 12, s1: 3 },
      { x: at('2026-10-09T10:10:00Z'), s0: 9, s1: null },
    ])
  })

  it('fills no bins when there would be too many to draw', () => {
    const second: TableColumn = { ...TIME, bin_seconds: 1 }
    const model = plot(chartOf(table([second, COUNT], [['2026-10-09T10:00:00Z', 1]], {
      fill_gaps: true, from: '2026-10-01T00:00:00Z', to: '2026-10-09T12:00:00Z',
    })))

    expect(model.points).toHaveLength(1)
  })
})

describe('many series', () => {
  const services = Array.from({ length: MAX_SERIES + 4 }, (_, i) => `svc-${String(i).padStart(2, '0')}`)
  // svc-00 has the fewest Logs, the last the most.
  const rows = services.flatMap((s, i) => [['2026-10-09T10:00:00Z', s, i + 1], ['2026-10-09T10:05:00Z', s, i + 1]])

  it('keeps the series with the largest totals, largest first, and says how many were left out', () => {
    const model = plot(chartOf(table([TIME, SERVICE, COUNT], rows)))

    expect(model.series.map(s => s.name)).toEqual(services.slice(4).reverse())
    expect(model.series[0].total).toBe(2 * (MAX_SERIES + 4))
    expect(model.omitted).toBe(4)
    expect(Object.keys(model.points[0])).toHaveLength(MAX_SERIES + 1)
  })

  it('keeps every number column of the split values it keeps', () => {
    const wide = services.flatMap((s, i) => [['2026-10-09T10:00:00Z', s, i + 1, 10]])
    const model = plot(chartOf(table([TIME, SERVICE, COUNT, AVG], wide)))

    expect(model.series.map(s => s.name)).toEqual(services.slice(-MAX_SERIES / 2).reverse().flatMap(s => [`${s} · count_`, `${s} · avg_duration_ms`]))
    expect(model.omitted).toBe(2 * (MAX_SERIES + 4) - MAX_SERIES)
  })

  it('colours the series with the palette in order, never twice', () => {
    const model = plot(chartOf(table([TIME, SERVICE, COUNT], rows)))

    expect(model.series.map(s => s.color)).toEqual(Array.from({ length: MAX_SERIES }, (_, i) => `var(--chart-${i + 1})`))
  })
})

describe('series of Levels', () => {
  const LEVEL: TableColumn = { name: 'level', type: 'string' }

  it('wear their Level\'s colour, fatal first so it stacks on the axis', () => {
    const model = plot(chartOf(table([TIME, LEVEL, COUNT], [
      ['2026-10-09T10:00:00Z', 'info', 50],
      ['2026-10-09T10:00:00Z', 'error', 4],
      ['2026-10-09T10:00:00Z', null, 2],
      ['2026-10-09T10:00:00Z', 'fatal', 1],
      ['2026-10-09T10:00:00Z', 'debug', 9],
    ])))

    expect(model.series.map(s => [s.name, s.color])).toEqual([
      ['fatal', 'var(--chart-level-fatal)'],
      ['error', 'var(--chart-level-error)'],
      ['info', 'var(--chart-level-info)'],
      ['debug', 'var(--chart-level-debug)'],
      ['no Level', 'var(--chart-level-none)'],
    ])
  })

  it('wear the palette when another column splits them too, or there are several numbers', () => {
    const split = plot(chartOf(table([TIME, LEVEL, SERVICE, COUNT], [['2026-10-09T10:00:00Z', 'info', 'api', 5]])))
    const twoNumbers = plot(chartOf(table([TIME, LEVEL, COUNT, AVG], [['2026-10-09T10:00:00Z', 'info', 5, 3]])))

    expect(split.series[0].color).toBe('var(--chart-1)')
    expect(twoNumbers.series.map(s => s.color)).toEqual(['var(--chart-1)', 'var(--chart-2)'])
  })
})

describe('bars', () => {
  it('draw a summarize with no time bin, a bar per value of its first string column', () => {
    const model = plot(chartOf(table([SERVICE, COUNT], [['api', 12], ['billing', 3], ['web', 7]])))

    expect(model).toMatchObject({ kind: 'bars', category: 'service', omitted: 0, omittedCategories: 0 })
    expect(model.series.map(s => s.name)).toEqual(['count_'])
    expect(model.points).toEqual([{ x: 'api', s0: 12 }, { x: 'billing', s0: 3 }, { x: 'web', s0: 7 }])
  })

  it('split each bar by the other string columns', () => {
    const LEVEL: TableColumn = { name: 'level', type: 'string' }
    const model = plot(chartOf(table([SERVICE, LEVEL, COUNT], [
      ['api', 'info', 10], ['api', 'error', 2], ['web', 'info', 7],
    ])))

    expect(model.series.map(s => s.name)).toEqual(['error', 'info'])
    expect(model.points).toEqual([{ x: 'api', s0: 2, s1: 10 }, { x: 'web', s0: null, s1: 7 }])
  })

  it('keep the categories with the largest totals, in the table\'s order', () => {
    const rows = Array.from({ length: MAX_BARS + 2 }, (_, i) => [`user-${i}`, i === 0 ? 1000 : i])
    const model = plot(chartOf(table([{ name: 'user', type: 'string' }, COUNT], rows)))

    expect(model.points).toHaveLength(MAX_BARS)
    expect(model.points[0].x).toBe('user-0')
    expect(model.points.map(p => p.x)).not.toContain('user-1')
    expect(model).toMatchObject({ omittedCategories: 2 })
  })

  it('need a summarize: a table with no aggregate is not drawn', () => {
    expect(chartOf(table([SERVICE, { name: 'duration_ms', type: 'number' }], [['api', 12]])).kind).toBe('none')
  })
})

describe('stacking', () => {
  it('stacks counts and sums of one column', () => {
    expect(plot(chartOf(table([TIME, SERVICE, COUNT], [['2026-10-09T10:00:00Z', 'api', 1]]))).stacked).toBe(true)
    expect(plot(chartOf(table([SERVICE, { name: 's', type: 'number', agg: 'sum' }], [['api', 1]]))).stacked).toBe(true)
  })

  it('never stacks values that do not add up, or two columns', () => {
    expect(plot(chartOf(table([TIME, SERVICE, AVG], [['2026-10-09T10:00:00Z', 'api', 1]]))).stacked).toBe(false)
    expect(plot(chartOf(table([TIME, { name: 'd', type: 'number', agg: 'dcount' }], [['2026-10-09T10:00:00Z', 1]]))).stacked).toBe(false)
    expect(plot(chartOf(table([TIME, COUNT, { name: 's', type: 'number', agg: 'sum' }], [['2026-10-09T10:00:00Z', 1, 2]]))).stacked).toBe(false)
  })
})

describe('a single number', () => {
  it('is a stat: one row with one number', () => {
    expect(chartOf(table([COUNT], [[1415]]))).toEqual({ kind: 'stat', name: 'count_', value: 1415 })
    expect(chartOf(table([{ name: 'Count', type: 'number' }], [[null]]))).toEqual({ kind: 'stat', name: 'Count', value: null })
  })
})

describe('tables that are not drawn', () => {
  it('include Log rows, rows with no number, and no rows at all', () => {
    expect(chartOf(table([{ name: 'message', type: 'string' }, SERVICE], [['boom', 'api']])).kind).toBe('none')
    expect(chartOf(table([TIME, SERVICE], [['2026-10-09T10:00:00Z', 'api']])).kind).toBe('none')
    expect(chartOf(table([TIME, COUNT], [])).kind).toBe('none')
    expect(chartOf(table([COUNT, AVG], [[1, 2]])).kind).toBe('none')
  })

  it('include a time bin with a column it cannot place', () => {
    const other: TableColumn = { name: 'first_seen', type: 'datetime' }
    const tags: TableColumn = { name: 'tags', type: 'dynamic' }

    expect(chartOf(table([TIME, other, COUNT], [['2026-10-09T10:00:00Z', '2026-10-09T10:01:00Z', 1]])).kind).toBe('none')
    expect(chartOf(table([TIME, tags, COUNT], [['2026-10-09T10:00:00Z', ['a'], 1]])).kind).toBe('none')
  })

  it('include two rows for one series in one bin, which a chart cannot tell apart', () => {
    expect(chartOf(table([TIME, COUNT], [['2026-10-09T10:00:00Z', 1], ['2026-10-09T10:00:00Z', 2]])).kind).toBe('none')
  })
})

describe('the forms a model is drawn in', () => {
  it('offers line, area and bars for time, bars for categories and a stat for a number', () => {
    expect(formsOf(chartOf(table([TIME, COUNT], [['2026-10-09T10:00:00Z', 1]])))).toEqual(['line', 'area', 'bars'])
    expect(formsOf(chartOf(table([SERVICE, COUNT], [['api', 1]])))).toEqual(['bars'])
    expect(formsOf(chartOf(table([COUNT], [[1]])))).toEqual(['stat'])
    expect(formsOf({ kind: 'none' })).toEqual([])
  })

  it('draws the form asked for when it can, else the one its shape suggests', () => {
    const time = chartOf(table([TIME, COUNT], [['2026-10-09T10:00:00Z', 1]]))
    const bars = chartOf(table([SERVICE, COUNT], [['api', 1]]))

    expect(formFor(time, 'auto')).toBe('line')
    expect(formFor(time, 'area')).toBe('area')
    expect(formFor(bars, 'line')).toBe('bars')
    expect(formFor(chartOf(table([COUNT], [[1]])), 'bars')).toBe('stat')
  })

  it('draws nothing for the table alone, or a table that is not drawn', () => {
    expect(formFor(chartOf(table([TIME, COUNT], [['2026-10-09T10:00:00Z', 1]])), 'table')).toBeNull()
    expect(formFor({ kind: 'none' }, 'line')).toBeNull()
  })
})
