// Runs Queries through Console's proxy of EventStore. Cancel by aborting the
// signal: EventStore can't stop a scan, so cancel only drops the answer.
import type { Chips } from './builder'
import type { Histogram } from './histogram'
import type { Log, LogPage } from './stream'
import type { LogTable } from './table'

export interface QueryProblem {
  message: string
  line: number
  column: number
  end_line: number
  end_column: number
}

/** Why a run gave no Logs; problems are a Query's mistakes, with positions. */
export class RunError extends Error {
  readonly problems: QueryProblem[]

  constructor(message: string, problems: QueryProblem[] = []) {
    super(message)
    this.problems = problems
  }
}

export interface LogPageRequest {
  orgSlug: string
  projectSlug: string
  query: string
  from: Date
  to: Date
  cursor?: string | null
  signal?: AbortSignal
}

/** What a run answers: Log rows, or a table for a tabular Query. */
export type RunAnswer = { kind: 'logs'; page: LogPage } | { kind: 'table'; table: LogTable }

/** A run's first answer: its newest page of Logs, or its table. */
export async function fetchRun(request: LogPageRequest): Promise<RunAnswer> {
  const body = await runQuery(request)
  if (body && typeof body.table === 'object' && body.table !== null) return { kind: 'table', table: body.table as LogTable }
  return { kind: 'logs', page: body as LogPage }
}

/** A page of Logs: older ones, from a run's cursor. */
export async function fetchLogPage(request: LogPageRequest): Promise<LogPage> {
  const body = await runQuery(request)
  if (Array.isArray(body?.logs)) return body as LogPage
  throw new RunError('The Query answered a table where Logs were expected.')
}

async function runQuery({ orgSlug, projectSlug, query, from, to, cursor, signal }: LogPageRequest) {
  const params = new URLSearchParams({ query, from: from.toISOString(), to: to.toISOString() })
  if (cursor) params.set('cursor', cursor)

  let response: Response
  try {
    response = await fetch(`/${orgSlug}/projects/${projectSlug}/logs/query?${params}`, {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
      signal,
    })
  } catch (error) {
    if (signal?.aborted) throw error
    throw new RunError('Console could not be reached. Check your connection and run again.')
  }

  const body = await response.json().catch(() => null)
  if (response.ok && body && (Array.isArray(body.logs) || (body.table && typeof body.table === 'object'))) return body
  throw runFailure(body, response.status)
}

export interface HistogramRequest {
  orgSlug: string
  projectSlug: string
  query: string
  from: Date
  to: Date
  bucketSeconds: number
  signal?: AbortSignal
}

/** The Logs of a run's range that pass its Query's filters, counted per bucket and Level. */
export async function fetchHistogram({ orgSlug, projectSlug, query, from, to, bucketSeconds, signal }: HistogramRequest): Promise<Histogram> {
  const params = new URLSearchParams({
    query, from: from.toISOString(), to: to.toISOString(), bucket_seconds: String(bucketSeconds),
  })
  const response = await get(`/${orgSlug}/projects/${projectSlug}/logs/histogram?${params}`, signal)
  const body = await response.json().catch(() => null)
  if (response.ok && body && Array.isArray(body.buckets)) return body as Histogram
  throw runFailure(body, response.status)
}

/** A failed run's error: the Query's first mistake, or what went wrong. */
function runFailure(body: { errors?: unknown; error?: unknown } | null, status: number): RunError {
  const problems: QueryProblem[] = Array.isArray(body?.errors) ? body.errors : []
  if (problems.length > 0) {
    const p = problems[0]
    return new RunError(`Line ${p.line}, column ${p.column}: ${p.message}`, problems)
  }
  return new RunError(typeof body?.error === 'string' ? body.error : `The Query failed (HTTP ${status}).`)
}

/** The Query as readable SQL against the logs view of [from, to), to seed SQL mode. */
export async function fetchReadableSql({ orgSlug, projectSlug, query, from, to, signal }: {
  orgSlug: string; projectSlug: string; query: string; from: Date; to: Date; signal?: AbortSignal
}): Promise<string> {
  const params = new URLSearchParams({ query, from: from.toISOString(), to: to.toISOString() })
  const response = await get(`/${orgSlug}/projects/${projectSlug}/logs/readable_sql?${params}`, signal)
  const body = await response.json().catch(() => null)
  if (response.ok && body && typeof body.sql === 'string') return body.sql
  throw runFailure(body, response.status)
}

/**
 * Runs SQL as written against the logs view of the Project and [from, to),
 * answering a table. A POST with the page's CSRF token, so only the page runs
 * SQL, never a link.
 */
export async function runSql({ orgSlug, projectSlug, sql, from, to, signal }: {
  orgSlug: string; projectSlug: string; sql: string; from: Date; to: Date; signal?: AbortSignal
}): Promise<LogTable> {
  const token = document.querySelector<HTMLMetaElement>('meta[name="csrf-token"]')?.content ?? ''
  let response: Response
  try {
    response = await fetch(`/${orgSlug}/projects/${projectSlug}/logs/sql`, {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': token },
      credentials: 'same-origin',
      body: JSON.stringify({ sql, from: from.toISOString(), to: to.toISOString() }),
      signal,
    })
  } catch (error) {
    if (signal?.aborted) throw error
    throw new RunError('Console could not be reached. Check your connection and run again.')
  }
  const body = await response.json().catch(() => null)
  if (response.ok && body?.table && typeof body.table === 'object') return body.table as LogTable
  throw new RunError(typeof body?.error === 'string' ? body.error : `The SQL failed (HTTP ${response.status}).`)
}

/**
 * Where validate places one stage of a Query, from its keyword to its end
 * (lines and columns from 1, columns in characters). filter: one of the
 * leading where and search stages; builder: the Builder can show it.
 */
export interface QueryStage {
  line: number
  column: number
  end_line: number
  end_column: number
  filter: boolean
  builder: boolean
}

export interface Validation {
  errors: QueryProblem[]
  /** The Query as Builder chips, or null when the Builder can't show it. */
  chips: Chips | null
  /** Each stage of a Query without mistakes; none otherwise. */
  stages: QueryStage[]
}

/** Checks a Query without running it; the Builder reads its chips from here. */
export async function validateQuery({ orgSlug, projectSlug, query, signal }: {
  orgSlug: string; projectSlug: string; query: string; signal?: AbortSignal
}): Promise<Validation> {
  const response = await get(`/${orgSlug}/projects/${projectSlug}/logs/validate?${new URLSearchParams({ query })}`, signal)
  const body = await response.json().catch(() => null)
  if (response.ok && body && Array.isArray(body.errors)) return body as Validation
  throw new RunError(typeof body?.error === 'string' ? body.error : `The Query could not be checked (HTTP ${response.status}).`)
}

/** One Log by id, or null if it's gone. time, the Log's own, narrows the search. */
export async function fetchLog({ orgSlug, projectSlug, id, time, signal }: {
  orgSlug: string; projectSlug: string; id: string; time?: string; signal?: AbortSignal
}): Promise<Log | null> {
  const params = time ? `?${new URLSearchParams({ time })}` : ''
  const response = await get(`/${orgSlug}/projects/${projectSlug}/logs/${encodeURIComponent(id)}${params}`, signal)
  if (response.status === 404) return null
  const body = await response.json().catch(() => null)
  if (response.ok && body && typeof body.id === 'string') return body as Log
  throw new RunError(typeof body?.error === 'string' ? body.error : `The Log could not be loaded (HTTP ${response.status}).`)
}

async function get(url: string, signal?: AbortSignal): Promise<Response> {
  try {
    return await fetch(url, { headers: { Accept: 'application/json' }, credentials: 'same-origin', signal })
  } catch (error) {
    if (signal?.aborted) throw error
    throw new RunError('Console could not be reached. Check your connection and try again.')
  }
}
