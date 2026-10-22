// Calls to Console for Dashboards that the pages make with fetch rather than
// an Inertia visit: a chart's data, which the fetch queue runs, the chart
// editor's preview, a field select's values, and the explorer's Add to dashboard and the grid's layout,
// which stay on the page.
import { RunError, runFailure, type TableAnswer } from '../logs/api'
import { elapsedOf } from '../logs/runLine'
import type { LogTable } from '../logs/table'
import {
  appendValues, type ChartOptions, type Dashboard, type DashboardChart, type DashboardViz, type FieldValue, type FilterValues, type Place,
} from './dashboard'

/**
 * A chart's saved Query run over [from, to) with the values of the filters it
 * names, as `v_<name>` keys: its table. The Query is read on the server, never sent.
 */
export async function fetchChartData({ orgSlug, dashboardId, chartId, from, to, parameters = {}, signal }: {
  orgSlug: string; dashboardId: number; chartId: number; from: Date; to: Date; parameters?: FilterValues; signal?: AbortSignal
}): Promise<TableAnswer> {
  const params = new URLSearchParams({ from: from.toISOString(), to: to.toISOString() })
  appendValues(params, parameters)
  return tableOf(await call(`/${orgSlug}/dashboards/${dashboardId}/charts/${chartId}/data?${params}`, { signal }))
}

/**
 * A select of a field's values: the field's 100 most common values in the
 * filter's Project over [from, to), most common first, run as a chart is.
 */
export async function fetchFilterValues({ orgSlug, dashboardId, name, from, to, signal }: {
  orgSlug: string; dashboardId: number; name: string; from: Date; to: Date; signal?: AbortSignal
}): Promise<FieldValue[]> {
  const params = new URLSearchParams({ from: from.toISOString(), to: to.toISOString() })
  const response = await call(`/${orgSlug}/dashboards/${dashboardId}/filters/${name}/values?${params}`, { signal })
  const body = await response.json().catch(() => null)
  if (response.ok && Array.isArray(body?.values)) return body.values as FieldValue[]
  throw runFailure(body, response.status)
}

export interface PreviewRun {
  projectId: number; query: string; from: Date; to: Date
  /** The filter bar's values, for the chart editor's preview. */
  parameters?: FilterValues
  signal?: AbortSignal
}

/** The chart editor's preview, through fetchPreview. */
export async function fetchChartPreview({ orgSlug, dashboardId, ...run }: { orgSlug: string; dashboardId: number } & PreviewRun): Promise<TableAnswer> {
  return fetchPreview(`/${orgSlug}/dashboards/${dashboardId}/charts/preview`, run)
}

/**
 * A preview: a Query not yet saved, run on a Project over [from, to) as a chart's data is, with its
 * timeout and within Console's cap on chart runs. The Query goes in the body. The chart editor's
 * and the Monitor form's preview endpoints answer it.
 */
export async function fetchPreview(url: string, { projectId, query, from, to, parameters, signal }: PreviewRun): Promise<TableAnswer> {
  return tableOf(await call(url, {
    method: 'POST',
    signal,
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken() },
    body: JSON.stringify({ project_id: projectId, query, from: from.toISOString(), to: to.toISOString(), parameters }),
  }))
}

async function tableOf(response: Response): Promise<TableAnswer> {
  const body = await response.json().catch(() => null)
  if (response.ok && body?.table && typeof body.table === 'object') return { table: body.table as LogTable, elapsedMs: elapsedOf(body) }
  throw runFailure(body, response.status)
}

/** Makes a Dashboard with a name, as the explorer's Add to dashboard does. */
export async function createDashboard(orgSlug: string, name: string): Promise<Dashboard> {
  return send<Dashboard>('POST', `/${orgSlug}/dashboards`, { dashboard: { name } })
}

export interface ChartInput {
  project_id: number
  title: string
  query: string
  viz: DashboardViz
  options?: ChartOptions
}

/** Adds a chart under a Dashboard's others. */
export async function addChart(orgSlug: string, dashboardId: number, chart: ChartInput): Promise<DashboardChart> {
  return send<DashboardChart>('POST', `/${orgSlug}/dashboards/${dashboardId}/charts`, { chart })
}

/** Saves the places of charts moved or resized on the grid, all or none; answers the Dashboard. */
export async function saveLayout(orgSlug: string, dashboardId: number, places: Place[]): Promise<Dashboard> {
  return send<Dashboard>('PATCH', `/${orgSlug}/dashboards/${dashboardId}/layout`, { layout: places })
}

/** A refusal's errors as a sentence: "Name can't be blank." */
export function errorsText(errors: Record<string, string[]>): string {
  return Object.entries(errors)
    .map(([field, messages]) => `${field[0].toUpperCase()}${field.slice(1).replaceAll('_', ' ')} ${messages[0]}.`)
    .join(' ')
}

async function send<T>(method: string, url: string, body: unknown): Promise<T> {
  const response = await call(url, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken() },
    body: JSON.stringify(body),
  })
  const answer = await response.json().catch(() => null)
  if (response.ok) return answer as T
  if (answer?.errors && typeof answer.errors === 'object' && !Array.isArray(answer.errors)) throw new RunError(errorsText(answer.errors))
  throw new RunError(typeof answer?.error === 'string' ? answer.error : `Console answered HTTP ${response.status}. Try again.`)
}

function csrfToken(): string {
  return document.querySelector<HTMLMetaElement>('meta[name="csrf-token"]')?.content ?? ''
}

async function call(url: string, init: RequestInit & { signal?: AbortSignal }): Promise<Response> {
  try {
    return await fetch(url, { ...init, headers: { Accept: 'application/json', ...init.headers }, credentials: 'same-origin' })
  } catch (error) {
    if (init.signal?.aborted) throw error
    throw new RunError('Console could not be reached. Check your connection and try again.')
  }
}
