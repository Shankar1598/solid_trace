// A Dashboard and its charts as the pages know them, and the plain logic
// around them: the range and filter values a page shows, how a chart's viz
// maps to the explorer's chart forms, a chart's link to the explorer, and the
// places a grid change moves charts to.
import type { AutoRefresh } from '../autoRefresh'
import type { ChartForm } from '../charts/series'
import { searchOf, stateFromSearch, type Viz } from '../logs/explorerState'
import type { QueryProblem } from '../logs/api'
import { parametersOf, tokenize } from '../logs/queryLexer'
import { isSpan, type TimeRange } from '../timeRange'

export type DashboardViz = 'line' | 'area' | 'bar' | 'stat' | 'table'
export const DASHBOARD_VIZES: DashboardViz[] = ['line', 'area', 'bar', 'stat', 'table']

export type { AutoRefresh } from '../autoRefresh'

/** How a chart reads its values; each only when set. */
export interface ChartOptions {
  /** false never stacks; otherwise series stack where their values add up. */
  stacked?: boolean
  unit?: string
  decimals?: number
}

export interface DashboardChart {
  id: number
  dashboard_id: number
  project_id: number
  title: string
  query: string
  viz: DashboardViz
  options: ChartOptions
  x: number
  y: number
  w: number
  h: number
  updated_at: string
}

export interface Dashboard {
  id: number
  name: string
  description: string | null
  /** A span back from now, such as `1h`. */
  default_range: string
  auto_refresh: AutoRefresh
  /** The filter bar's controls, in order. */
  filters: DashboardFilter[]
  chart_count: number
  created_by: string | null
  created_at: string
  updated_at: string
  charts?: DashboardChart[]
}

// Types rather than interfaces, so a list of filters is Inertia form data.
export type FilterOption = { label: string; value: string }

/**
 * A select's options: a fixed list, or a field's most common values in one
 * of the Organization's Projects, over the range the Dashboard shows.
 */
export type FilterOptions = { from: 'list'; items: FilterOption[] } | { from: 'field'; project_id: number; field: string }

/** One of a field's values, and how many Logs hold it; null for a chosen value that wasn't loaded. */
export type FieldValue = { value: string; count: number | null }

/**
 * One of a Dashboard's filters, which a chart's Query names as `$name`: a
 * select of one value or several, or a search box. Its label is never empty.
 */
export type DashboardFilter =
  | { name: string; label: string; kind: 'select'; multiple: boolean; options: FilterOptions }
  | { name: string; label: string; kind: 'search' }

/** The values chosen in a Dashboard's filters, by name. A filter left out is All. */
export type FilterValues = Record<string, string[]>

/** A chart's place on the 12-column grid. */
export interface Place {
  id: number
  x: number
  y: number
  w: number
  h: number
}

export const GRID_COLUMNS = 12

/** The range a Dashboard's URL holds, else its default range (`1h` when that isn't a span). */
export function rangeOfSearch(search: string, defaultRange: string): TimeRange {
  const params = new URLSearchParams(search)
  if (params.has('range') || (params.has('from') && params.has('to'))) return stateFromSearch(search).range
  return { kind: 'relative', span: isSpan(defaultRange) ? defaultRange : '1h' }
}

/**
 * A range, and the filters' values beside it, as a Dashboard's URL search:
 * relative stays relative, and each value is a `v_<name>` key, repeated for
 * several values.
 */
export function searchOfRange(range: TimeRange, values: FilterValues = {}): string {
  const params = new URLSearchParams()
  if (range.kind === 'relative') {
    params.set('range', range.span)
  } else {
    params.set('from', range.from.toISOString())
    params.set('to', range.to.toISOString())
  }
  appendValues(params, values)
  return `?${params}`
}

/** Each value as a `v_<name>` key. */
export function appendValues(params: URLSearchParams, values: FilterValues) {
  for (const [name, list] of Object.entries(values)) {
    for (const value of list) params.append(`v_${name}`, value)
  }
}

/**
 * The values of a Dashboard's filters, in the filters' order, from values by
 * name: names that aren't its filters and All (no value, or empty) are left
 * out, and a filter that takes one value keeps the first.
 */
export function filterValues(filters: DashboardFilter[], given: Record<string, readonly string[] | undefined>): FilterValues {
  const values: FilterValues = {}
  for (const filter of filters) {
    const list = (given[filter.name] ?? []).filter(value => value !== '')
    const kept = filter.kind === 'select' && filter.multiple ? list : list.slice(0, 1)
    if (kept.length > 0) values[filter.name] = kept
  }
  return values
}

/** The values a Dashboard's URL holds for its filters. */
export function filterValuesOfSearch(search: string, filters: DashboardFilter[]): FilterValues {
  const params = new URLSearchParams(search)
  return filterValues(filters, Object.fromEntries(filters.map(f => [f.name, params.getAll(`v_${f.name}`)])))
}

/** The values a chart's Query runs with: those of the Dashboard's filters it names that aren't All. */
export function parametersOfChart(query: string, filters: DashboardFilter[], values: FilterValues): FilterValues {
  const named = new Set(parametersOf(query))
  return filterValues(filters.filter(f => named.has(f.name)), values)
}

/**
 * A problem at each $name in a chart's Query that names, among parameters
 * (the names validate found), a filter the Dashboard lacks: the chart editor
 * won't save it. Lines and columns count from 1, columns in characters, as
 * validate's do.
 */
export function unknownFilterProblems(query: string, parameters: string[], filters: DashboardFilter[]): QueryProblem[] {
  const known = new Set(filters.map(f => f.name))
  const unknown = new Set(parameters.filter(name => !known.has(name)))
  const names = filters.map(f => f.name).join(', ')
  const messageOf = (name: string) =>
    `$${name} isn't one of this Dashboard's filters${names === '' ? '; it has none' : ` (${names})`}.`
  const problems: QueryProblem[] = []
  const placed = new Set<string>()
  for (const token of tokenize(query)) {
    const name = token.text.slice(1)
    if (token.kind !== 'parameter' || !unknown.has(name)) continue
    const from = placeOf(query, token.from)
    const to = placeOf(query, token.to)
    problems.push({ message: messageOf(name), line: from.line, column: from.column, end_line: to.line, end_column: to.column })
    placed.add(name)
  }
  // A name validate found that the browser's lexer didn't still stops a save.
  for (const name of unknown) {
    if (!placed.has(name)) problems.push({ message: messageOf(name), line: 1, column: 1, end_line: 1, end_column: 1 })
  }
  return problems
}

/** An offset into text as a line and a column from 1, the column in characters. */
function placeOf(text: string, offset: number): { line: number; column: number } {
  const lines = text.slice(0, offset).split('\n')
  return { line: lines.length, column: [...lines[lines.length - 1]].length + 1 }
}

/** The names a chart's Query uses that aren't the Dashboard's filters, which run as All. */
export function unknownFilters(query: string, filters: DashboardFilter[]): string[] {
  const known = new Set(filters.map(f => f.name))
  return parametersOf(query).filter(name => !known.has(name))
}

/** What a filter holds, as its bar reads it: All, up to two values by their labels, or a count. */
export function chosenText(filter: DashboardFilter, values: readonly string[]): string {
  if (values.length === 0) return 'All'
  if (values.length > 2) return `${values.length} values`
  const labels = new Map(filter.kind === 'select' && filter.options.from === 'list' ? filter.options.items.map(o => [o.value, o.label]) : [])
  return values.map(value => labels.get(value) ?? value).join(', ')
}

/**
 * What a select of a field's values offers: the values loaded, most common
 * first, after the chosen values that aren't among them, so a value from a
 * link still shows as chosen. An empty value is All, so it isn't one.
 */
export function fieldOptions(loaded: FieldValue[], chosen: readonly string[]): FieldValue[] {
  const known = new Set(loaded.map(v => v.value))
  const missing = chosen.filter(value => !known.has(value)).map(value => ({ value, count: null }))
  return [...missing, ...loaded.filter(v => v.value !== '')]
}

const LABELLED_OPTION = /^(.*?)\s+=\s+(.*)$/

/** A select's options typed one per line, each a value or `Label = value`; blank lines go. */
export function optionsOfText(text: string): FilterOption[] {
  return text.split('\n').map(line => line.trim()).filter(line => line !== '').map(line => {
    const labelled = LABELLED_OPTION.exec(line)
    return labelled ? { label: labelled[1], value: labelled[2] } : { label: line, value: line }
  })
}

/** Options as optionsOfText reads them. */
export function textOfOptions(items: FilterOption[]): string {
  return items.map(o => (o.label === o.value ? o.value : `${o.label} = ${o.value}`)).join('\n')
}

/** The chart form a viz draws, or the table alone. */
export function formOfViz(viz: DashboardViz): ChartForm | 'table' {
  return viz === 'bar' ? 'bars' : viz
}

/** The viz that draws a form the explorer showed. */
export function vizOfForm(form: ChartForm | 'table'): DashboardViz {
  return form === 'bars' ? 'bar' : form
}

const EXPLORER_VIZ: Record<DashboardViz, Viz> = { line: 'line', area: 'area', bar: 'bars', stat: 'auto', table: 'table' }

/** The explorer over a chart's Project, Query and the range shown, drawn as the chart is. */
export function explorerHref(orgSlug: string, projectSlug: string, chart: Pick<DashboardChart, 'query' | 'viz'>, range: TimeRange): string {
  const search = searchOf({ range, mode: 'builder', query: chart.query, sql: '', viz: EXPLORER_VIZ[chart.viz] })
  return `/${orgSlug}/projects/${projectSlug}/logs${search}`
}

const SOURCE = /^\s*\[\s*'(?:[^'\\]|\\.)*'\s*\]/

/** A Query reading `slug`'s Logs: its source, when it names one, becomes `['slug']`. */
export function withSource(query: string, slug: string): string {
  return SOURCE.test(query) ? query.replace(SOURCE, `['${slug.replaceAll('\\', '\\\\').replaceAll("'", "\\'")}']`) : query
}

/** The first Query of a new chart: Logs counted over time. */
export function newChartQuery(slug: string): string {
  return withSource(`['']\n| summarize count() by bin_auto(time)`, slug)
}

/** A title for a chart made from a Query: its last stage, cut to 80 characters. */
export function titleOfQuery(query: string): string {
  const stages = query.split('|').map(s => s.replace(/\s+/g, ' ').trim()).filter(s => s !== '' && !SOURCE.test(s))
  const last = stages.at(-1) ?? ''
  if (last === '') return 'Logs'
  return last.length > 80 ? `${last.slice(0, 79)}…` : last
}

/** The places in `layout` that differ from the charts', for one layout update. */
export function movedPlaces(charts: Place[], layout: { i: string; x: number; y: number; w: number; h: number }[]): Place[] {
  const byId = new Map(charts.map(c => [String(c.id), c]))
  return layout.flatMap(item => {
    const chart = byId.get(item.i)
    if (!chart || (chart.x === item.x && chart.y === item.y && chart.w === item.w && chart.h === item.h)) return []
    return [{ id: chart.id, x: item.x, y: item.y, w: item.w, h: item.h }]
  })
}
