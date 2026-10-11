// A Query's table as a chart: a time series, bars or a single number. Pure,
// so the Logs explorer, Dashboards and Monitors draw a table the same way.
// EventStore's hints say which column holds the time bins (bin_seconds),
// which columns are aggregates (agg) and whether an empty bin means no Logs
// (fill_gaps). Tables come long, one row per bin and split value; a chart
// wants them wide, one point per bin with a value per series.
import type { Agg, LogTable } from '@/lib/logs/table'

/** The most series a chart draws: one per hue of the palette, never a made-up ninth. */
export const MAX_SERIES = 8
/** The most bars a chart of categories draws. */
export const MAX_BARS = 50
/** The most bins a time series lays; past it, only the ends of each run of bins with no row are. */
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
  /**
   * What the series is, whatever its rank: its split values, and its column
   * unless each panel draws one column. Its colour and legend entry follow it.
   */
  id: string
  /** Its palette slot (1 to MAX_SERIES), or null when it wears a Level's colour. */
  slot: number | null
  /** A CSS colour: its palette slot's hue, or a Level's own colour. */
  color: string
  total: number
}

/** A legend entry: one per series id, showing and hiding every series of it. */
export interface LegendEntry {
  id: string
  name: string
  color: string
}

/**
 * Series drawn over one value axis: those whose columns measure one thing,
 * such as counts of Logs, or the min, avg, max and percentiles of one Field.
 */
export interface Panel {
  /** The number columns it draws. */
  columns: string[]
  /** Its series' keys. */
  keys: string[]
  /** Whether its series add up to a whole, so bars and areas stack. */
  stacked: boolean
}

/** One bin or category: x is a bin's start (epoch ms) or a category's name. */
export type ChartPoint = { x: number | string } & { [key: string]: number | string | null }

interface SeriesSet {
  series: Series[]
  points: ChartPoint[]
  /** Series left out past MAX_SERIES, those of number columns past it included. */
  omitted: number
  /** Number columns left out past MAX_SERIES. */
  omittedColumns: number
  /** The names of series left out because they have no value at all. */
  empty: string[]
  /** The series' value axes, top to bottom: one unless the columns measure different things. */
  panels: Panel[]
  legend: LegendEntry[]
  /** The rows the table was cut at, when EventStore cut it: the chart draws only those. */
  cutAt: number | null
}

export type ChartModel =
  | { kind: 'none' }
  | { kind: 'stat'; name: string; value: number | null }
  | ({
    kind: 'time'
    binSeconds: number
    from: Date
    to: Date
    /** The bins (their starts) at the range's ends that it covers only in part, whose values count part of a bin. */
    partial: number[]
  } & SeriesSet)
  | ({ kind: 'bars'; category: string; omittedCategories: number } & SeriesSet)

/** A model drawn with axes: a time series or bars. */
export type SeriesChartModel = Extract<ChartModel, { kind: 'time' | 'bars' }>

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
 * Whether a Query's table will likely be drawn over time: its last summarize
 * groups by a bin of time. A guess from the text, for before it answers.
 */
export function binsTime(query: string): boolean {
  const at = query.lastIndexOf('summarize')
  return at >= 0 && /\bbin(_auto)?\s*\(\s*time\b/.test(query.slice(at))
}

/**
 * The chart a table makes:
 * - a time series when it has the last summarize's time bin column: number
 *   columns are series, and string and bool columns split them. Every bin
 *   from the first to the last has a point, so a line breaks where a bin has
 *   no row; when the table says such a bin has no Logs, a count there is
 *   zero, so a count over no Logs at all is a flat zero;
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

  const binAt = columns.findIndex(c => c.type === 'datetime' && c.bin_seconds)
  const roles = rolesOf(table, binAt)
  if (!roles) return { kind: 'none' }
  const { measures, splits } = roles
  if (measures.length === 0) return { kind: 'none' }
  if (rows.length === 0 && !(binAt >= 0 && zeroWhenEmpty(table, measures, splits))) return { kind: 'none' }

  if (binAt >= 0) {
    const binSeconds = columns[binAt].bin_seconds!
    const step = binSeconds * 1000
    const filled = table.fill_gaps === true
    const pivot = pivotRows(table, i => {
      const time = rows[i][binAt]
      return typeof time === 'string' ? new Date(time).getTime() : null
    }, measures, splits)
    if (!pivot) return { kind: 'none' }
    const drawn = keepSeries(pivot, table, measures, splits, filled)
    if (drawn.series.length === 0) return { kind: 'none' }
    const times = [...pivot.xs.keys()].map(Number).sort((a, b) => a - b)
    const from = table.from ? new Date(table.from) : new Date(times[0])
    // A cut table has only its oldest bins, so its time axis ends at its last.
    const end = times.length > 0 ? times[times.length - 1] + step : 0
    const to = new Date(Math.min(table.to ? Date.parse(table.to) : end, table.truncated ? end : Infinity))
    // With no Logs in an empty bin, the bins run across the range; else between the rows'.
    if (filled) layBins(drawn, step, Math.floor(from.getTime() / step) * step, Math.ceil(to.getTime() / step) * step - step, true)
    else if (times.length > 0) layBins(drawn, step, times[0], times[times.length - 1], false)
    drawn.points.sort((a, b) => (a.x as number) - (b.x as number))
    const edges = [drawn.points[0]?.x as number, drawn.points[drawn.points.length - 1]?.x as number]
    const partial = [...new Set(edges)].filter(x => x !== undefined && (x < from.getTime() || x + step > to.getTime()))
    return { kind: 'time', binSeconds, from, to, partial, ...drawn }
  }

  // Bars: only a summarize's table, with a column to name each bar.
  if (!columns.some(c => c.agg) || splits.length === 0) return { kind: 'none' }
  const [category, ...rest] = splits
  const pivot = pivotRows(table, i => splitName(columns[category].name, cell(rows[i][category])), measures, rest)
  if (!pivot) return { kind: 'none' }
  const drawn = keepSeries(pivot, table, measures, rest, false)
  if (drawn.series.length === 0) return { kind: 'none' }
  // The bars with the largest totals, largest first; Levels and numbers keep their own order.
  const omittedCategories = Math.max(0, drawn.points.length - MAX_BARS)
  const sum = (p: ChartPoint) => drawn.series.reduce((s, series) => s + (typeof p[series.key] === 'number' ? (p[series.key] as number) : 0), 0)
  const byTotal = [...drawn.points].sort((a, b) => sum(b) - sum(a)).slice(0, MAX_BARS)
  const natural = naturalOrder(table, category)
  drawn.points = natural ? byTotal.sort((a, b) => natural(a.x as string) - natural(b.x as string)) : byTotal
  return { kind: 'bars', category: columns[category].name, omittedCategories, ...drawn }
}

/**
 * Where a bar's category falls when its values have an order of their own:
 * Levels worst first, as the histogram stacks them, and numbers (status
 * codes) ascending. Null when they are names, which bars rank by total.
 */
function naturalOrder({ columns, rows }: LogTable, category: number): ((name: string) => number) | null {
  const field = columns[category].name
  const values = rows.map(row => cell(row[category]))
  const place = new Map<string, number>()
  if (field === 'level' && values.every(v => v === null || LEVEL_ORDER.includes(v as never))) {
    for (const v of values) place.set(splitName(field, v), v === null ? LEVEL_ORDER.length : LEVEL_ORDER.indexOf(v as never))
  } else if (values.every(v => v === null || typeof v === 'number')) {
    for (const v of values) place.set(splitName(field, v), v === null ? Infinity : (v as number))
  } else {
    return null
  }
  return name => place.get(name) ?? Infinity
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
  // Nothing splitting the series leaves one, even with no rows.
  const combos: Pivot['combos'] = new Map(splits.length === 0 ? [['[]', []]] : [])
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
 * hue), each with every measure. A series with no value at all is left out
 * and named in empty. Series of Levels go worst first and wear their Level's
 * colour. A missing value is a zero where an empty bin has no Logs and the
 * aggregate counts, and a gap otherwise.
 */
function keepSeries(
  pivot: Pivot, { columns, rows, truncated }: LogTable, measures: number[], splits: number[], filled: boolean,
): SeriesSet {
  const drawn = measures.slice(0, MAX_SERIES)
  const source = (combo: string, m: number) => `${combo}\u0000${m}`
  const totals = new Map<string, number>()
  const valued = new Set<string>()
  for (const at of pivot.xs.values()) {
    for (const [key, value] of at) {
      totals.set(key, (totals.get(key) ?? 0) + (value ?? 0))
      if (value !== null) valued.add(key)
    }
  }
  // A series with no value in any row draws nothing, so it gets no hue; with no rows at all, every series is a zero.
  const hasValues = (combo: string, m: number) => rows.length === 0 || valued.has(source(combo, m))
  const nameOf = (combo: string, m: number) => {
    const parts = splits.map((s, k) => splitName(columns[s].name, pivot.combos.get(combo)![k]))
    if (drawn.length > 1 || parts.length === 0) parts.push(columns[m].name)
    return parts.join(' · ')
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

  // Columns of one unit share a panel. When each panel draws one column, a
  // split value wears one hue in all of them, so as many split values as hues are kept.
  const units = groupBy(drawn, m => unitOf(columns[m].name, columns[m].agg))
  const byCombo = splits.length > 0 && units.length > 1 && units.every(unit => unit.length === 1)
  const empty = order.flatMap(combo => drawn.filter(m => !hasValues(combo, m)).map(m => nameOf(combo, m)))
  const kept = order
    .filter(combo => drawn.some(m => hasValues(combo, m)))
    .sort((a, b) => rank(a) - rank(b) || order.indexOf(a) - order.indexOf(b))
    .slice(0, byCombo ? MAX_SERIES : Math.max(1, Math.floor(MAX_SERIES / drawn.length)))

  const series: (Series & { source: string })[] = []
  const slots = new Map<string, number>()
  for (const combo of kept) {
    const values = pivot.combos.get(combo)!
    const split = splits.map((s, k) => ({ field: columns[s].name, value: values[k] }))
    for (const m of drawn.filter(m => hasValues(combo, m))) {
      const id = JSON.stringify(byCombo ? split : [split, columns[m].name])
      if (!slots.has(id)) slots.set(id, slots.size + 1)
      const slot = levels ? null : slots.get(id)!
      series.push({
        key: `s${series.length}`,
        name: byCombo ? split.map(({ field, value }) => splitName(field, value)).join(' · ') : nameOf(combo, m),
        column: columns[m].name,
        split,
        agg: columns[m].agg ?? null,
        id,
        slot,
        color: slot === null ? `var(--chart-level-${levelOf(combo) ?? 'none'})` : `var(--chart-${slot})`,
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

  const panels = units.map((unit): Panel => {
    const agg = unit.length === 1 ? columns[unit[0]].agg : undefined
    const names = unit.map(m => columns[m].name)
    return {
      columns: names,
      keys: series.filter(s => names.includes(s.column)).map(s => s.key),
      stacked: agg !== undefined && ADDS_UP.has(agg),
    }
  }).filter(panel => panel.keys.length > 0)

  const drawnSeries = series.map(({ source: _, ...s }) => s)
  return {
    series: drawnSeries,
    points,
    omitted: pivot.combos.size * measures.length - series.length - empty.length,
    omittedColumns: measures.length - drawn.length,
    empty,
    panels,
    legend: legendOf(drawnSeries),
    cutAt: truncated ? rows.length : null,
  }
}

/** One legend entry per series id, in the series' order. */
function legendOf(series: Series[]): LegendEntry[] {
  const entries = new Map<string, LegendEntry>()
  for (const s of series) if (!entries.has(s.id)) entries.set(s.id, { id: s.id, name: s.name, color: s.color })
  return [...entries.values()]
}

/**
 * What a number column measures, so columns of one unit share an axis:
 * counts of Logs, or the values of the Field an avg, min, max or percentile
 * is named after (avg_duration_ms, percentile_duration_ms_95). Any other
 * column, a renamed one included, measures something of its own.
 */
function unitOf(name: string, agg: Agg | undefined): string {
  if (agg === 'count' || agg === 'countif') return 'count'
  const named = /^(avg|min|max|percentile)_(.+?)(_\d+(?:\.\d+)?)?$/.exec(name)
  return named && named[1] === agg ? `field ${named[2]}` : `column ${name}`
}

/** Items grouped by a key, in the order each key first appears. */
function groupBy<T>(items: T[], keyOf: (item: T) => string): T[][] {
  const groups = new Map<string, T[]>()
  for (const item of items) {
    const key = keyOf(item)
    groups.set(key, [...(groups.get(key) ?? []), item])
  }
  return [...groups.values()]
}

/**
 * The model coloured as the last chart drawn in its place was: a series id
 * drawn there keeps its palette slot, so filtering out a series or a change
 * of rank repaints none of the others, and a new id takes the lowest slot
 * free. A new shape matches no id, so it starts from slot 1 again. Returns
 * the slots to keep for the next.
 */
export function keepColors(model: ChartModel, slots: ReadonlyMap<string, number>): { model: ChartModel; slots: Map<string, number> } {
  if (model.kind !== 'time' && model.kind !== 'bars') return { model, slots: new Map(slots) }
  const ids = [...new Set(model.series.filter(s => s.slot !== null).map(s => s.id))]
  if (ids.length === 0) return { model, slots: new Map(slots) }
  const next = new Map<string, number>()
  for (const id of ids) if (slots.has(id)) next.set(id, slots.get(id)!)
  const taken = new Set(next.values())
  for (const id of ids) {
    if (next.has(id)) continue
    let slot = 1
    while (taken.has(slot)) slot++
    next.set(id, slot)
    taken.add(slot)
  }
  const series = model.series.map(s => (s.slot === null ? s : { ...s, slot: next.get(s.id)!, color: `var(--chart-${next.get(s.id)!})` }))
  return { model: { ...model, series, legend: legendOf(series) }, slots: next }
}

/**
 * Adds a point for each bin from first to last (bin starts, epoch ms) that
 * has no row: a zero or a gap, as emptyValue says. Past MAX_FILLED_BINS only
 * the first and last bins of each run without rows get one, which is enough
 * to break a line, or drop it to zero, across the run.
 */
function layBins(drawn: SeriesSet, step: number, first: number, last: number, filled: boolean) {
  const have = new Set(drawn.points.map(p => p.x as number))
  const missing: number[] = []
  if ((last - first) / step <= MAX_FILLED_BINS) {
    for (let x = first; x <= last; x += step) if (!have.has(x)) missing.push(x)
  } else {
    const xs = [...have].sort((a, b) => a - b)
    const ends = filled ? [first - step, ...xs, last + step] : xs
    for (let i = 1; i < ends.length; i++) {
      const [a, b] = [ends[i - 1], ends[i]]
      if (b - a <= step) continue
      missing.push(a + step)
      if (b - step > a + step) missing.push(b - step)
    }
  }
  for (const x of missing) {
    const point: ChartPoint = { x }
    for (const s of drawn.series) point[s.key] = emptyValue(s, filled)
    drawn.points.push(point)
  }
}

/**
 * Whether a table with no rows is a flat zero: its bins have no Logs, and
 * each number column is a count or sum of them, with nothing splitting it.
 */
function zeroWhenEmpty(table: LogTable, measures: number[], splits: number[]): boolean {
  return table.fill_gaps === true && !!table.from && !!table.to && splits.length === 0 &&
    measures.every(m => ZERO_WHEN_EMPTY.has(table.columns[m].agg as Agg))
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

/** A note beside a chart's legend: what it says, and why, for its title. */
export interface LegendNote {
  text: string
  why: string
}

/** What a chart leaves out, and why: series past the palette, bars past MAX_BARS, series with no values, rows past a cut. */
export function legendNotes(model: SeriesChartModel): LegendNote[] {
  const notes: LegendNote[] = []
  if (model.omitted > 0) {
    const columns = [...new Set(model.series.map(s => s.column))]
    const fields = model.series[0].split.map(f => f.field)
    const why: string[] = []
    if (fields.length > 0) {
      const kept = new Set(model.series.map(s => JSON.stringify(s.split))).size
      const each = columns.length > 1 ? `, each with its ${columns.length} number columns` : ''
      why.push(`Only the ${kept} ${fields.join(' and ')} ${kept === 1 ? 'value' : 'values'} with the largest ${columns[0]} totals are drawn${each}.`)
    }
    if (model.omittedColumns > 0) why.push(`Only the first ${columns.length} number columns are drawn.`)
    notes.push({ text: `${model.omitted.toLocaleString()} more not shown`, why: why.join(' ') })
  }
  if (model.kind === 'bars' && model.omittedCategories > 0) {
    const all = model.points.length + model.omittedCategories
    notes.push({
      text: `the top ${model.points.length} of ${all.toLocaleString()} ${model.category} values`,
      why: `Only the ${model.points.length} bars with the largest totals are drawn.`,
    })
  }
  if (model.empty.length > 0) {
    const n = model.empty.length
    const names = n > 4 ? `${model.empty.slice(0, 3).join(', ')} and ${n - 3} more` : listOf(model.empty)
    const where = model.kind === 'time' ? 'in any bin' : `for any ${model.category}`
    notes.push({ text: `${n} with no values`, why: `${names} ${n === 1 ? 'has' : 'have'} no value ${where}, so ${n === 1 ? 'it is' : 'they are'} not drawn.` })
  }
  if (model.cutAt !== null) {
    const rows = model.cutAt.toLocaleString()
    const lost = model.kind === 'time' ? 'its later bins are missing, and ' : ''
    notes.push({ text: `drawn from the first ${rows} rows`, why: `The table was cut at ${rows} rows, so ${lost}the totals that rank what is drawn count only those rows.` })
  }
  return notes
}

function listOf(names: string[]): string {
  return names.length < 2 ? names.join('') : `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`
}
