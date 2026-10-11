import { describe, expect, it } from 'vitest'
import type { LogTable, TableColumn } from '@/lib/logs/table'
import { MAX_BARS, MAX_SERIES, chartOf, formFor, formsOf, keepColors, legendNotes, type ChartModel } from './series'

const TIME: TableColumn = { name: 'time', type: 'datetime', bin_seconds: 300 }
const SERVICE: TableColumn = { name: 'service', type: 'string' }
const COUNT: TableColumn = { name: 'count_', type: 'number', agg: 'count' }
const AVG: TableColumn = { name: 'avg_duration_ms', type: 'number', agg: 'avg' }
const COUNTIF: TableColumn = { name: 'countif_', type: 'number', agg: 'countif' }

const at = (time: string) => new Date(time).getTime()

function table(columns: TableColumn[], rows: unknown[][], extra: Partial<LogTable> = {}): LogTable {
  return { columns, rows, truncated: false, ...extra }
}

/** The model's series and points, for a time series or bars. */
function drawable(model: ChartModel) {
  if (model.kind !== 'time' && model.kind !== 'bars') throw new Error(`expected series, got ${model.kind}`)
  return model
}

describe('a time series', () => {
  it('draws each number column over the bin column, oldest first', () => {
    const model = drawable(chartOf(table([TIME, COUNT, AVG], [
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
    const model = drawable(chartOf(table([TIME, SERVICE, COUNT], [
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

  it('names a series by its split values and, with several number columns over one axis, its column', () => {
    const ok: TableColumn = { name: 'ok', type: 'bool' }
    const model = drawable(chartOf(table([TIME, SERVICE, ok, COUNT, COUNTIF], [
      ['2026-10-09T10:00:00Z', 'api', true, 12, 40],
      ['2026-10-09T10:00:00Z', null, false, 1, 9],
    ])))

    // Split values by the total of the first number column, then each number column in the table's order.
    expect(model.series.map(s => s.name)).toEqual([
      'api · true · count_', 'api · true · countif_', 'no service · false · count_', 'no service · false · countif_',
    ])
    expect(model.series.find(s => s.name === 'no service · false · count_')?.split).toEqual([
      { field: 'service', value: null }, { field: 'ok', value: false },
    ])
  })

  it('splits by a number column that comes before the aggregates, as a by column does', () => {
    const status: TableColumn = { name: 'status', type: 'number' }
    const model = drawable(chartOf(table([TIME, status, COUNT], [
      ['2026-10-09T10:00:00Z', 200, 12],
      ['2026-10-09T10:00:00Z', 500, 2],
    ])))

    expect(model.series.map(s => s.name)).toEqual(['200', '500'])
  })

  it('draws a number column after the aggregates, such as arithmetic over them', () => {
    const rate: TableColumn = { name: 'per_minute', type: 'number' }
    const model = drawable(chartOf(table([TIME, COUNT, rate], [['2026-10-09T10:00:00Z', 10, 2]])))

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

describe('a by column over an Attribute', () => {
  // EventStore types a by column over an Attribute as dynamic: its cells are the JSON values the Logs hold.
  const path: TableColumn = { name: 'http.path', type: 'dynamic' }
  const status: TableColumn = { name: 'http.status', type: 'dynamic' }

  it('splits a time series by its values', () => {
    const model = drawable(chartOf(table([TIME, path, COUNT], [
      ['2026-10-09T10:00:00Z', '/orders', 12],
      ['2026-10-09T10:00:00Z', null, 3],
      ['2026-10-09T10:05:00Z', '/orders', 9],
    ])))

    expect(model.kind).toBe('time')
    expect(model.series.map(s => ({ name: s.name, split: s.split }))).toEqual([
      { name: '/orders', split: [{ field: 'http.path', value: '/orders' }] },
      { name: 'no http.path', split: [{ field: 'http.path', value: null }] },
    ])
  })

  it('names the bars of a summarize with no time bin, numbers and all', () => {
    const model = drawable(chartOf(table([status, COUNT], [[200, 40], [500, 3]])))

    expect(model).toMatchObject({ kind: 'bars', category: 'http.status' })
    expect(model.points.map(p => p.x)).toEqual(['200', '500'])
  })

  it('is not drawn when a value is a list or an object, which no series can be named by', () => {
    expect(chartOf(table([TIME, path, COUNT], [['2026-10-09T10:00:00Z', ['a'], 1]])).kind).toBe('none')
    expect(chartOf(table([path, COUNT], [[{ a: 1 }, 1]])).kind).toBe('none')
  })
})

describe('filling empty bins', () => {
  const range = { from: '2026-10-09T09:58:00Z', to: '2026-10-09T10:15:00Z' }

  it('fills the bins from the range\'s first to its last with zero counts', () => {
    const model = drawable(chartOf(table([TIME, COUNT], [['2026-10-09T10:05:00Z', 4]], { fill_gaps: true, ...range })))

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
    const model = drawable(chartOf(table(columns, [['2026-10-09T10:05:00Z', 1, 1, 1, 1, 1, 1, 1]], { fill_gaps: true, ...range })))

    expect(model.points[1]).toEqual({ x: at('2026-10-09T10:00:00Z'), s0: 0, s1: 0, s2: 0, s3: null, s4: null, s5: null, s6: null })
  })

  it('fills a series missing from a bin the same way', () => {
    const model = drawable(chartOf(table([TIME, SERVICE, COUNT], [
      ['2026-10-09T10:00:00Z', 'api', 12],
      ['2026-10-09T10:00:00Z', 'web', 3],
      ['2026-10-09T10:05:00Z', 'api', 9],
    ], { fill_gaps: true, ...range })))

    expect(model.points.find(p => p.x === at('2026-10-09T10:05:00Z'))).toEqual({ x: at('2026-10-09T10:05:00Z'), s0: 9, s1: 0 })
  })

  it('leaves a gap in a bin with no row when the table says it may have Logs', () => {
    const model = drawable(chartOf(table([TIME, SERVICE, COUNT], [
      ['2026-10-09T10:00:00Z', 'api', 12],
      ['2026-10-09T10:00:00Z', 'web', 3],
      ['2026-10-09T10:10:00Z', 'api', 9],
    ], { fill_gaps: false, ...range })))

    // From the first row's bin to the last: nothing is known beyond them.
    expect(model.points).toEqual([
      { x: at('2026-10-09T10:00:00Z'), s0: 12, s1: 3 },
      { x: at('2026-10-09T10:05:00Z'), s0: null, s1: null },
      { x: at('2026-10-09T10:10:00Z'), s0: 9, s1: null },
    ])
  })

  it('marks only the ends of each run of empty bins when there would be too many to lay', () => {
    const second: TableColumn = { ...TIME, bin_seconds: 1 }
    const filled = drawable(chartOf(table([second, COUNT], [['2026-10-09T10:00:00Z', 1]], {
      fill_gaps: true, from: '2026-10-01T00:00:00Z', to: '2026-10-09T12:00:00Z',
    })))
    const gaps = drawable(chartOf(table([second, COUNT], [['2026-10-01T00:00:00Z', 1], ['2026-10-09T10:00:00Z', 2]])))

    expect(filled.points).toEqual([
      { x: at('2026-10-01T00:00:00Z'), s0: 0 },
      { x: at('2026-10-09T09:59:59Z'), s0: 0 },
      { x: at('2026-10-09T10:00:00Z'), s0: 1 },
      { x: at('2026-10-09T10:00:01Z'), s0: 0 },
      { x: at('2026-10-09T11:59:59Z'), s0: 0 },
    ])
    expect(gaps.points).toEqual([
      { x: at('2026-10-01T00:00:00Z'), s0: 1 },
      { x: at('2026-10-01T00:00:01Z'), s0: null },
      { x: at('2026-10-09T09:59:59Z'), s0: null },
      { x: at('2026-10-09T10:00:00Z'), s0: 2 },
    ])
  })

  it('draws a count over no Logs as a flat zero across the range', () => {
    const model = drawable(chartOf(table([TIME, COUNT], [], { fill_gaps: true, ...range })))

    expect(model.series.map(s => s.name)).toEqual(['count_'])
    expect(model.points.map(p => p.s0)).toEqual([0, 0, 0, 0])
  })

  it('draws nothing for no rows it cannot fill: no hint, a gap aggregate, or split values it cannot know', () => {
    expect(chartOf(table([TIME, COUNT], [], range)).kind).toBe('none')
    expect(chartOf(table([TIME, AVG], [], { fill_gaps: true, ...range })).kind).toBe('none')
    expect(chartOf(table([TIME, SERVICE, COUNT], [], { fill_gaps: true, ...range })).kind).toBe('none')
  })
})

describe('many series', () => {
  const services = Array.from({ length: MAX_SERIES + 4 }, (_, i) => `svc-${String(i).padStart(2, '0')}`)
  // svc-00 has the fewest Logs, the last the most.
  const rows = services.flatMap((s, i) => [['2026-10-09T10:00:00Z', s, i + 1], ['2026-10-09T10:05:00Z', s, i + 1]])

  it('keeps the series with the largest totals, largest first, and says how many were left out', () => {
    const model = drawable(chartOf(table([TIME, SERVICE, COUNT], rows)))

    expect(model.series.map(s => s.name)).toEqual(services.slice(4).reverse())
    expect(model.series[0].total).toBe(2 * (MAX_SERIES + 4))
    expect(model.omitted).toBe(4)
    expect(Object.keys(model.points[0])).toHaveLength(MAX_SERIES + 1)
  })

  it('keeps every number column of the split values it keeps, a hue each over one axis', () => {
    const wide = services.flatMap((s, i) => [['2026-10-09T10:00:00Z', s, i + 1, 10]])
    const model = drawable(chartOf(table([TIME, SERVICE, COUNT, COUNTIF], wide)))

    expect(model.series.map(s => s.name)).toEqual(services.slice(-MAX_SERIES / 2).reverse().flatMap(s => [`${s} · count_`, `${s} · countif_`]))
    expect(model.omitted).toBe(2 * (MAX_SERIES + 4) - MAX_SERIES)
  })

  it('keeps as many split values as hues when each panel draws one column', () => {
    const wide = services.flatMap((s, i) => [['2026-10-09T10:00:00Z', s, i + 1, 10]])
    const model = drawable(chartOf(table([TIME, SERVICE, COUNT, AVG], wide)))

    expect(model.legend.map(e => e.name)).toEqual(services.slice(-MAX_SERIES).reverse())
    expect(model.series).toHaveLength(2 * MAX_SERIES)
    expect(model.omitted).toBe(2 * 4)
  })

  it('say how many split values are drawn, with every number column', () => {
    const three = services.slice(0, 5).flatMap((s, i) => [['2026-10-09T10:00:00Z', s, i + 1, 10, 20]])
    const model = drawable(chartOf(table([TIME, SERVICE, COUNT, AVG, { name: 'max_duration_ms', type: 'number', agg: 'max' }], three)))

    expect(model.series).toHaveLength(6)
    expect(legendNotes(model)).toEqual([{
      text: '9 more not shown',
      why: 'Only the 2 service values with the largest count_ totals are drawn, each with its 3 number columns.',
    }])
  })

  it('say when number columns past the palette are left out', () => {
    const columns = Array.from({ length: MAX_SERIES + 2 }, (_, i): TableColumn => ({ name: `c${i}`, type: 'number', agg: 'sum' }))
    const model = drawable(chartOf(table([TIME, ...columns], [['2026-10-09T10:00:00Z', ...columns.map(() => 1)]])))

    expect(legendNotes(model)).toEqual([{ text: '2 more not shown', why: `Only the first ${MAX_SERIES} number columns are drawn.` }])
  })

  it('colours the series with the palette in order, never twice', () => {
    const model = drawable(chartOf(table([TIME, SERVICE, COUNT], rows)))

    expect(model.series.map(s => s.color)).toEqual(Array.from({ length: MAX_SERIES }, (_, i) => `var(--chart-${i + 1})`))
  })
})

describe('series with no values', () => {
  const P95: TableColumn = { name: 'percentile_duration_ms_95', type: 'number', agg: 'percentile' }

  it('are left out, named, and give their hue to a series that draws', () => {
    const model = drawable(chartOf(table([TIME, SERVICE, P95], [
      ['2026-10-09T10:00:00Z', 'billing', null],
      ['2026-10-09T10:00:00Z', 'api', 120],
      ['2026-10-09T10:00:00Z', 'web', null],
      ['2026-10-09T10:05:00Z', 'worker', 80],
    ])))

    expect(model.series.map(s => [s.name, s.color])).toEqual([['api', 'var(--chart-1)'], ['worker', 'var(--chart-2)']])
    expect(model.empty).toEqual(['billing', 'web'])
    expect(model.omitted).toBe(0)
    expect(legendNotes(model)).toEqual([{ text: '2 with no values', why: 'billing and web have no value in any bin, so they are not drawn.' }])
  })

  it('leave nothing to draw when every series is one', () => {
    expect(chartOf(table([TIME, AVG], [['2026-10-09T10:00:00Z', null]])).kind).toBe('none')
  })
})

describe('a table EventStore cut', () => {
  it('ends its time axis at its last bin and says it was cut', () => {
    const model = drawable(chartOf(table([TIME, COUNT], [['2026-10-09T10:00:00Z', 3], ['2026-10-09T10:05:00Z', 4]], {
      truncated: true, from: '2026-10-09T09:58:00Z', to: '2026-10-09T12:00:00Z',
    })))

    expect(model).toMatchObject({ kind: 'time', to: new Date('2026-10-09T10:10:00Z'), cutAt: 2 })
    expect(legendNotes(model)).toEqual([{
      text: 'drawn from the first 2 rows',
      why: 'The table was cut at 2 rows, so its later bins are missing, and the totals that rank what is drawn count only those rows.',
    }])
  })
})

describe('series of Levels', () => {
  const LEVEL: TableColumn = { name: 'level', type: 'string' }

  it('wear their Level\'s colour, fatal first so it stacks on the axis', () => {
    const model = drawable(chartOf(table([TIME, LEVEL, COUNT], [
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
    const split = drawable(chartOf(table([TIME, LEVEL, SERVICE, COUNT], [['2026-10-09T10:00:00Z', 'info', 'api', 5]])))
    const twoNumbers = drawable(chartOf(table([TIME, LEVEL, COUNT, COUNTIF], [['2026-10-09T10:00:00Z', 'info', 5, 3]])))

    expect(split.series[0].color).toBe('var(--chart-1)')
    expect(twoNumbers.series.map(s => s.color)).toEqual(['var(--chart-1)', 'var(--chart-2)'])
  })
})

describe('bars', () => {
  it('draw a summarize with no time bin, a bar per value of its first string column', () => {
    const model = drawable(chartOf(table([SERVICE, COUNT], [['api', 12], ['billing', 3], ['web', 7]])))

    expect(model).toMatchObject({ kind: 'bars', category: 'service', omitted: 0, omittedCategories: 0 })
    expect(model.series.map(s => s.name)).toEqual(['count_'])
    // Largest first, so the bars read as a ranking.
    expect(model.points).toEqual([{ x: 'api', s0: 12 }, { x: 'web', s0: 7 }, { x: 'billing', s0: 3 }])
  })

  it('keep the order of Levels and of numbers, which have one of their own', () => {
    const LEVEL: TableColumn = { name: 'level', type: 'string' }
    const status: TableColumn = { name: 'http.status', type: 'dynamic' }
    const levels = drawable(chartOf(table([LEVEL, COUNT], [['debug', 40], ['error', 3], ['info', 90], [null, 5]])))
    const statuses = drawable(chartOf(table([status, COUNT], [[500, 2], [200, 90], [404, 7]])))

    expect(levels.points.map(p => p.x)).toEqual(['error', 'info', 'debug', 'no Level'])
    expect(statuses.points.map(p => p.x)).toEqual(['200', '404', '500'])
  })

  it('split each bar by the other string columns', () => {
    const LEVEL: TableColumn = { name: 'level', type: 'string' }
    const model = drawable(chartOf(table([SERVICE, LEVEL, COUNT], [
      ['api', 'info', 10], ['api', 'error', 2], ['web', 'info', 7],
    ])))

    expect(model.series.map(s => s.name)).toEqual(['error', 'info'])
    expect(model.points).toEqual([{ x: 'api', s0: 2, s1: 10 }, { x: 'web', s0: null, s1: 7 }])
  })

  it('keep the categories with the largest totals, largest first', () => {
    const rows = Array.from({ length: MAX_BARS + 2 }, (_, i) => [`user-${i}`, i === 0 ? 1000 : i])
    const model = drawable(chartOf(table([{ name: 'user', type: 'string' }, COUNT], rows)))

    expect(model.points).toHaveLength(MAX_BARS)
    expect(model.points.slice(0, 3).map(p => p.x)).toEqual(['user-0', `user-${MAX_BARS + 1}`, `user-${MAX_BARS}`])
    expect(model.points.map(p => p.x)).not.toContain('user-1')
    expect(model).toMatchObject({ omittedCategories: 2 })
    expect(legendNotes(model)).toEqual([{ text: `the top ${MAX_BARS} of ${MAX_BARS + 2} user values`, why: `Only the ${MAX_BARS} bars with the largest totals are drawn.` }])
  })

  it('need a summarize: a table with no aggregate is not drawn', () => {
    expect(chartOf(table([SERVICE, { name: 'duration_ms', type: 'number' }], [['api', 12]])).kind).toBe('none')
  })
})

describe('stacking', () => {
  const stacked = (model: ChartModel) => drawable(model).panels.map(p => p.stacked)

  it('stacks counts and sums of one column', () => {
    expect(stacked(chartOf(table([TIME, SERVICE, COUNT], [['2026-10-09T10:00:00Z', 'api', 1]])))).toEqual([true])
    expect(stacked(chartOf(table([SERVICE, { name: 's', type: 'number', agg: 'sum' }], [['api', 1]])))).toEqual([true])
  })

  it('never stacks values that do not add up, or two columns over one axis', () => {
    expect(stacked(chartOf(table([TIME, SERVICE, AVG], [['2026-10-09T10:00:00Z', 'api', 1]])))).toEqual([false])
    expect(stacked(chartOf(table([TIME, { name: 'd', type: 'number', agg: 'dcount' }], [['2026-10-09T10:00:00Z', 1]])))).toEqual([false])
    expect(stacked(chartOf(table([TIME, COUNT, COUNTIF], [['2026-10-09T10:00:00Z', 2, 1]])))).toEqual([false])
  })
})

describe('panels', () => {
  const P95: TableColumn = { name: 'percentile_duration_ms_95', type: 'number', agg: 'percentile' }
  const MAX: TableColumn = { name: 'max_duration_ms', type: 'number', agg: 'max' }

  it('give columns that measure different things an axis each', () => {
    const model = drawable(chartOf(table([TIME, AVG, COUNT], [['2026-10-09T10:00:00Z', 2500, 12]])))

    expect(model.panels).toEqual([
      { columns: ['avg_duration_ms'], keys: ['s0'], stacked: false },
      { columns: ['count_'], keys: ['s1'], stacked: true },
    ])
  })

  it('share one axis between counts, and between an avg, max and percentile of one Field', () => {
    const countif: TableColumn = { name: 'countif_', type: 'number', agg: 'countif' }
    const model = drawable(chartOf(table([TIME, COUNT, AVG, countif, P95, MAX], [['2026-10-09T10:00:00Z', 9, 30, 2, 80, 120]])))

    expect(model.panels.map(p => p.columns)).toEqual([
      ['count_', 'countif_'], ['avg_duration_ms', 'percentile_duration_ms_95', 'max_duration_ms'],
    ])
  })

  it('give a renamed column, a sum or arithmetic over aggregates an axis of its own', () => {
    const p95: TableColumn = { name: 'p95', type: 'number', agg: 'percentile' }
    const sum: TableColumn = { name: 'sum_duration_ms', type: 'number', agg: 'sum' }
    const rate: TableColumn = { name: 'rate', type: 'number' }
    const model = drawable(chartOf(table([TIME, AVG, p95, sum, rate], [['2026-10-09T10:00:00Z', 1, 2, 3, 4]])))

    expect(model.panels.map(p => p.columns)).toEqual([['avg_duration_ms'], ['p95'], ['sum_duration_ms'], ['rate']])
  })

  it('colour a split value alike in every panel, with a legend entry each, when each panel draws one column', () => {
    const model = drawable(chartOf(table([TIME, SERVICE, AVG, COUNT], [
      ['2026-10-09T10:00:00Z', 'api', 50, 12],
      ['2026-10-09T10:00:00Z', 'web', 30, 3],
    ])))

    expect(model.series.map(s => [s.name, s.column, s.color])).toEqual([
      ['api', 'avg_duration_ms', 'var(--chart-1)'], ['api', 'count_', 'var(--chart-1)'],
      ['web', 'avg_duration_ms', 'var(--chart-2)'], ['web', 'count_', 'var(--chart-2)'],
    ])
    expect(model.legend.map(e => [e.name, e.color])).toEqual([['api', 'var(--chart-1)'], ['web', 'var(--chart-2)']])
    expect(model.panels.map(p => p.keys)).toEqual([['s0', 's2'], ['s1', 's3']])
  })
})

describe('colours kept from the last chart', () => {
  const rows = (services: [string, number][]) => services.map(([s, n]) => ['2026-10-09T10:00:00Z', s, n])
  const colors = (model: ChartModel) => Object.fromEntries(drawable(model).legend.map(e => [e.name, e.color]))

  it('stay on a series when a filter or a new rank changes the others', () => {
    const first = keepColors(chartOf(table([TIME, SERVICE, COUNT], rows([['api', 40], ['worker', 30], ['web', 20], ['billing', 10]]))), new Map())
    expect(colors(first.model)).toEqual({ api: 'var(--chart-1)', worker: 'var(--chart-2)', web: 'var(--chart-3)', billing: 'var(--chart-4)' })

    // api hidden, and billing now outranks web.
    const filtered = keepColors(chartOf(table([TIME, SERVICE, COUNT], rows([['worker', 30], ['billing', 25], ['web', 20]]))), first.slots)
    expect(colors(filtered.model)).toEqual({ worker: 'var(--chart-2)', billing: 'var(--chart-4)', web: 'var(--chart-3)' })

    // api back, and a new service: the lowest slots free.
    const back = keepColors(chartOf(table([TIME, SERVICE, COUNT], rows([['api', 40], ['worker', 30], ['cron', 5]]))), filtered.slots)
    expect(colors(back.model)).toEqual({ api: 'var(--chart-1)', worker: 'var(--chart-2)', cron: 'var(--chart-3)' })
  })

  it('start from the first slot for a new shape, and leave Levels their own colours', () => {
    const services = keepColors(chartOf(table([TIME, SERVICE, COUNT], rows([['api', 40], ['web', 20]]))), new Map())
    const LEVEL: TableColumn = { name: 'level', type: 'string' }
    const levels = keepColors(chartOf(table([TIME, LEVEL, COUNT], rows([['error', 4]]))), services.slots)
    const hosts = keepColors(chartOf(table([TIME, { name: 'host', type: 'string' }, COUNT], rows([['h1', 3]]))), levels.slots)

    expect(colors(levels.model)).toEqual({ error: 'var(--chart-level-error)' })
    expect(levels.slots).toEqual(services.slots)
    expect(colors(hosts.model)).toEqual({ h1: 'var(--chart-1)' })
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
    expect(chartOf(table([SERVICE, COUNT], [], { fill_gaps: true, from: '2026-10-09T10:00:00Z', to: '2026-10-09T11:00:00Z' })).kind).toBe('none')
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
