// A Query's table as a chart: a time series, bars or a single number. Pure,
// so the Logs explorer, Dashboards and Monitors draw a table the same way.
// EventStore's hints say which column holds the time bins (bin_seconds),
// which columns are aggregates (agg) and whether an empty bin means no Logs
// (fill_gaps). Tables come long, one row per bin and split value; a chart
// wants them wide, one point per bin with a value per series.
import type { Agg, LogTable, TableColumn } from '@/lib/logs/table'

/** The most series a chart draws: one per hue of the palette, never a made-up ninth. */
export const MAX_SERIES = 8
/** The most bars a chart of categories draws. */
export const MAX_BARS = 50
/** The most bins a time series fills; past it, only the bins with rows are drawn. */
const MAX_FILLED_BINS = 10_000

/** Aggregates whose empty bin is a zero; the others have no value there. */
const ZERO_WHEN_EMPTY: ReadonlySet<Agg> = new Set(['count', 'countif', 'dcount', 'sum'])
/** Aggregates whose split values add up to the whole, so their series stack. */
const ADDS_UP: ReadonlySet<Agg> = new Set(['count', 'countif', 'sum'])

/** The Levels, worst first: the order series of Levels stack in, as the histogram's bars do. */
const LEVEL_ORDER = ['fatal', 'error', 'warn', 'info', 'debug', 'trace'] as const

export type SplitValue = string | number | boolean | null

export interface Series {
  /** Its values' key in each point. */
  key: string
  /** What the legend and tooltip call it. */
  name: string
  /** The number column it draws. */
  column: string
  /** The values of the columns that split it, which a Log has as Fields. */
  split: { field: string; value: SplitValue }[]
  agg: Agg | null
  /** A CSS colour: the palette's next hue, or a Level's own colour. */
  color: string
  total: number
}

/** One bin or category: x is a bin's start (epoch ms) or a category's name. */
export type ChartPoint = { x: number | string } & { [key: string]: number | string | null }

interface SeriesSet {
  series: Series[]
  points: ChartPoint[]
  /** Series left out past MAX_SERIES. */
  omitted: number
  /** Whether the series add up to a whole, so bars and areas stack. */
  stacked: boolean
}

export type ChartModel =
  | { kind: 'none' }
  | { kind: 'stat'; name: string; value: number | null }
  | ({ kind: 'time'; binSeconds: number; from: Date; to: Date } & SeriesSet)
  | ({ kind: 'bars'; category: string; omittedCategories: number } & SeriesSet)

/** How a model can be drawn. */
export type ChartForm = 'line' | 'area' | 'bars' | 'stat'

/** The forms a model can be drawn in, the one its shape suggests first. */
export function formsOf(model: ChartModel): ChartForm[] {
  switch (model.kind) {
    case 'time': return ['line', 'area', 'bars']
    case 'bars': return ['bars']
    case 'stat': return ['stat']
    case 'none': return []
  }
}

/**
 * The form to draw a model in: the one asked for when the model can be drawn
 * so, else the one its shape suggests; null for the table alone, or a table
 * that isn't drawn.
 */
export function formFor(model: ChartModel, wanted: ChartForm | 'auto' | 'table'): ChartForm | null {
  const forms = formsOf(model)
  if (wanted === 'table' || forms.length === 0) return null
  return forms.includes(wanted as ChartForm) ? (wanted as ChartForm) : forms[0]
}

/**
 * The chart a table makes:
 * - a time series when it has the last summarize's time bin column: number
 *   columns are series, and string and bool columns split them;
 * - bars when it is a summarize with no time bin: a bar per value of its
 *   first string column, split by the others;
 * - a stat when it is one row of one number;
 * - none otherwise.
 */
export function chartOf(table: LogTable): ChartModel {
  const { columns, rows } = table
  if (rows.length === 1 && columns.length === 1 && columns[0].type === 'number') {
    return { kind: 'stat', name: columns[0].name, value: numberOf(rows[0][0]) }
  }
  if (rows.length === 0) return { kind: 'none' }

  const binAt = columns.findIndex(c => c.type === 'datetime' && c.bin_seconds)
  const roles = rolesOf(table, binAt)
  if (!roles) return { kind: 'none' }
  const { measures, splits } = roles
  if (measures.length === 0) return { kind: 'none' }

  if (binAt >= 0) {
    const binSeconds = columns[binAt].bin_seconds!
    const pivot = pivotRows(table, i => {
      const time = rows[i][binAt]
      return typeof time === 'string' ? new Date(time).getTime() : null
    }, measures, splits)
    if (!pivot) return { kind: 'none' }
    const drawn = keepSeries(pivot, columns, measures, splits, table.fill_gaps === true)
    const times = [...pivot.xs.keys()].map(Number).sort((a, b) => a - b)
    const from = table.from ? new Date(table.from) : new Date(times[0])
    const to = table.to ? new Date(table.to) : new Date(times[times.length - 1] + binSeconds * 1000)
    if (table.fill_gaps) fillBins(drawn, binSeconds, from, to)
    drawn.points.sort((a, b) => (a.x as number) - (b.x as number))
    return { kind: 'time', binSeconds, from, to, ...drawn }
  }

  // Bars: only a summarize's table, with a column to name each bar.
  if (!columns.some(c => c.agg) || splits.length === 0) return { kind: 'none' }
  const [category, ...rest] = splits
  const pivot = pivotRows(table, i => splitName(columns[category].name, cell(rows[i][category])), measures, rest)
  if (!pivot) return { kind: 'none' }
  const drawn = keepSeries(pivot, columns, measures, rest, false)
  const omittedCategories = Math.max(0, drawn.points.length - MAX_BARS)
  if (omittedCategories > 0) {
    const sum = (p: ChartPoint) => drawn.series.reduce((s, series) => s + (typeof p[series.key] === 'number' ? (p[series.key] as number) : 0), 0)
    const kept = new Set([...drawn.points].sort((a, b) => sum(b) - sum(a)).slice(0, MAX_BARS))
    drawn.points = drawn.points.filter(p => kept.has(p))
  }
  return { kind: 'bars', category: columns[category].name, omittedCategories, ...drawn }
}

/**
 * Which columns are drawn (measures) and which split the series. A number
 * column is a measure, unless it comes before the aggregates, where a
 * summarize puts its by columns (such as a status code). A by column over an
 * Attribute comes as dynamic, and splits when its cells are plain values. Any
 * other column but the time bin can't be placed, so the table isn't drawn.
 */
function rolesOf({ columns, rows }: LogTable, binAt: number): { measures: number[]; splits: number[] } | null {
  const firstAgg = columns.findIndex(c => c.agg)
  const measures: number[] = []
  const splits: number[] = []
  for (let i = 0; i < columns.length; i++) {
    if (i === binAt) continue
    const { type, agg } = columns[i]
    if (type === 'number' && (firstAgg === -1 || agg || i > firstAgg)) measures.push(i)
    else if (type === 'number' || type === 'string' || type === 'bool') splits.push(i)
    else if (type === 'dynamic' && rows.every(row => row[i] === null || row[i] === undefined || cell(row[i]) !== null)) splits.push(i)
    else return null
  }
  return { measures, splits }
}

/** A table's rows pivoted wide: a value per x, split values and measure. */
interface Pivot {
  /** Each x, in the order of the rows. */
  xs: Map<number | string, Map<string, number | null>>
  /** Each combination of split values, in the order of the rows. */
  combos: Map<string, SplitValue[]>
}

/** Pivots the rows; null when two rows give one series two values at one x. */
function pivotRows(
  table: LogTable, xOf: (row: number) => number | string | null, measures: number[], splits: number[],
): Pivot | null {
  const xs: Pivot['xs'] = new Map()
  const combos: Pivot['combos'] = new Map()
  for (let i = 0; i < table.rows.length; i++) {
    const x = xOf(i)
    if (x === null || (typeof x === 'number' && isNaN(x))) continue
    const values = splits.map(s => cell(table.rows[i][s]))
    const combo = JSON.stringify(values)
    if (!combos.has(combo)) combos.set(combo, values)
    let at = xs.get(x)
    if (!at) xs.set(x, (at = new Map()))
    for (const m of measures) {
      const key = `${combo}\u0000${m}`
      if (at.has(key)) return null
      at.set(key, numberOf(table.rows[i][m]))
    }
  }
  return { xs, combos }
}

/**
 * The series to draw and their points: the combinations of split values with
 * the largest totals of the first measure (as many as leave every measure a
 * hue), each with every measure. Series of Levels go worst first and wear
 * their Level's colour. A missing value is a zero where an empty bin has no
 * Logs and the aggregate counts, and a gap otherwise.
 */
function keepSeries(pivot: Pivot, columns: TableColumn[], measures: number[], splits: number[], filled: boolean): SeriesSet {
  const drawn = measures.slice(0, MAX_SERIES)
  const source = (combo: string, m: number) => `${combo}\u0000${m}`
  const totals = new Map<string, number>()
  for (const at of pivot.xs.values()) {
    for (const [key, value] of at) totals.set(key, (totals.get(key) ?? 0) + (value ?? 0))
  }

  // Series of Levels: split by level alone, one measure, every value a Level or none.
  const order = [...pivot.combos.keys()]
  const levelOf = (combo: string) => pivot.combos.get(combo)![0]
  const levels = splits.length === 1 && columns[splits[0]].name === 'level' && drawn.length === 1 &&
    order.every(combo => levelOf(combo) === null || LEVEL_ORDER.includes(levelOf(combo) as never))
  const rank = (combo: string) => {
    if (!levels) return -(totals.get(source(combo, drawn[0])) ?? 0)
    const level = levelOf(combo)
    return level === null ? LEVEL_ORDER.length : LEVEL_ORDER.indexOf(level as never)
  }
  const kept = [...order]
    .sort((a, b) => rank(a) - rank(b) || order.indexOf(a) - order.indexOf(b))
    .slice(0, Math.max(1, Math.floor(MAX_SERIES / drawn.length)))

  const series: (Series & { source: string })[] = []
  for (const combo of kept) {
    const values = pivot.combos.get(combo)!
    const split = splits.map((s, k) => ({ field: columns[s].name, value: values[k] }))
    for (const m of drawn) {
      const parts = split.map(({ field, value }) => splitName(field, value))
      if (drawn.length > 1 || parts.length === 0) parts.push(columns[m].name)
      series.push({
        key: `s${series.length}`,
        name: parts.join(' · '),
        column: columns[m].name,
        split,
        agg: columns[m].agg ?? null,
        color: levels ? `var(--chart-level-${levelOf(combo) ?? 'none'})` : `var(--chart-${series.length + 1})`,
        total: totals.get(source(combo, m)) ?? 0,
        source: source(combo, m),
      })
    }
  }

  const points: ChartPoint[] = [...pivot.xs].map(([x, at]) => {
    const point: ChartPoint = { x }
    for (const s of series) point[s.key] = at.has(s.source) ? at.get(s.source)! : emptyValue(s, filled)
    return point
  })

  const agg = drawn.length === 1 ? columns[drawn[0]].agg : undefined
  return {
    series: series.map(({ source: _, ...s }) => s),
    points,
    omitted: pivot.combos.size * measures.length - series.length,
    stacked: agg !== undefined && ADDS_UP.has(agg),
  }
}

/** Adds the bins with no row from the range's first bin to its last, unless there would be too many. */
function fillBins(drawn: SeriesSet, binSeconds: number, from: Date, to: Date) {
  const step = binSeconds * 1000
  const first = Math.floor(from.getTime() / step) * step
  if ((to.getTime() - first) / step > MAX_FILLED_BINS) return
  const have = new Set(drawn.points.map(p => p.x))
  for (let x = first; x < to.getTime(); x += step) {
    if (have.has(x)) continue
    const point: ChartPoint = { x }
    for (const s of drawn.series) point[s.key] = emptyValue(s, true)
    drawn.points.push(point)
  }
}

function emptyValue(series: Series, filled: boolean): number | null {
  return filled && series.agg && ZERO_WHEN_EMPTY.has(series.agg) ? 0 : null
}

/** A split value as a legend shows it: no service for a Log without one. */
function splitName(field: string, value: SplitValue): string {
  if (value === null) return field === 'level' ? 'no Level' : `no ${field}`
  return String(value)
}

function cell(value: unknown): SplitValue {
  return typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean' ? value : null
}

function numberOf(value: unknown): number | null {
  if (typeof value === 'number') return Number.isFinite(value) ? value : null
  if (typeof value === 'string' && value.trim() !== '' && Number.isFinite(Number(value))) return Number(value)
  return null
}
