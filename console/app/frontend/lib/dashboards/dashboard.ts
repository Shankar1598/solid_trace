// A Dashboard and its charts as the pages know them, and the plain logic
// around them: the range a page shows, how a chart's viz maps to the
// explorer's chart forms, a chart's link to the explorer, and the places a
// grid change moves charts to.
import type { ChartForm } from '../charts/series'
import { searchOf, stateFromSearch, type Viz } from '../logs/explorerState'
import { isSpan, type TimeRange } from '../timeRange'

export type DashboardViz = 'line' | 'area' | 'bar' | 'stat' | 'table'
export const DASHBOARD_VIZES: DashboardViz[] = ['line', 'area', 'bar', 'stat', 'table']

export type AutoRefresh = 'off' | '1m' | '5m'
export const AUTO_REFRESH_MS: Record<AutoRefresh, number | null> = { off: null, '1m': 60_000, '5m': 300_000 }

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
  chart_count: number
  created_by: string | null
  created_at: string
  updated_at: string
  charts?: DashboardChart[]
}

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

/** A range as a Dashboard's URL search: relative stays relative. */
export function searchOfRange(range: TimeRange): string {
  const params = new URLSearchParams()
  if (range.kind === 'relative') {
    params.set('range', range.span)
  } else {
    params.set('from', range.from.toISOString())
    params.set('to', range.to.toISOString())
  }
  return `?${params}`
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
