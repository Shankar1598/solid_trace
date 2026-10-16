// Calls to Console for Dashboards that the pages make with fetch rather than
// an Inertia visit: a chart's data, which the fetch queue runs, and the
// explorer's Add to dashboard and the grid's layout, which stay on the page.
import { RunError, runFailure, type TableAnswer } from '../logs/api'
import { elapsedOf } from '../logs/runLine'
import type { LogTable } from '../logs/table'
import type { ChartOptions, Dashboard, DashboardChart, DashboardViz, Place } from './dashboard'

/** A chart's saved Query run over [from, to): its table. The Query is read on the server, never sent. */
export async function fetchChartData({ orgSlug, dashboardId, chartId, from, to, signal }: {
  orgSlug: string; dashboardId: number; chartId: number; from: Date; to: Date; signal?: AbortSignal
}): Promise<TableAnswer> {
  const params = new URLSearchParams({ from: from.toISOString(), to: to.toISOString() })
  const response = await call(`/${orgSlug}/dashboards/${dashboardId}/charts/${chartId}/data?${params}`, { signal })
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
  const token = document.querySelector<HTMLMetaElement>('meta[name="csrf-token"]')?.content ?? ''
  const response = await call(url, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': token },
    body: JSON.stringify(body),
  })
  const answer = await response.json().catch(() => null)
  if (response.ok) return answer as T
  if (answer?.errors && typeof answer.errors === 'object' && !Array.isArray(answer.errors)) throw new RunError(errorsText(answer.errors))
  throw new RunError(typeof answer?.error === 'string' ? answer.error : `Console answered HTTP ${response.status}. Try again.`)
}

async function call(url: string, init: RequestInit & { signal?: AbortSignal }): Promise<Response> {
  try {
    return await fetch(url, { ...init, headers: { Accept: 'application/json', ...init.headers }, credentials: 'same-origin' })
  } catch (error) {
    if (init.signal?.aborted) throw error
    throw new RunError('Console could not be reached. Check your connection and try again.')
  }
}
