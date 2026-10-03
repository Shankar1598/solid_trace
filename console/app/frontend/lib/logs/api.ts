// Runs Queries through Console's proxy of EventStore. Cancel by aborting the
// signal: EventStore can't stop a scan, so cancel only drops the answer.
import type { Chips } from './builder'
import type { Log, LogPage } from './stream'

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

export async function fetchLogPage({ orgSlug, projectSlug, query, from, to, cursor, signal }: LogPageRequest): Promise<LogPage> {
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
  if (response.ok && body && Array.isArray(body.logs)) return body as LogPage

  const problems: QueryProblem[] = Array.isArray(body?.errors) ? body.errors : []
  if (problems.length > 0) {
    const p = problems[0]
    throw new RunError(`Line ${p.line}, column ${p.column}: ${p.message}`, problems)
  }
  throw new RunError(typeof body?.error === 'string' ? body.error : `The Query failed (HTTP ${response.status}).`)
}

export interface Validation {
  errors: QueryProblem[]
  /** The Query as Builder chips, or null when the Builder can't show it. */
  chips: Chips | null
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
