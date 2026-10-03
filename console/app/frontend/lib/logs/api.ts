// Runs Queries through Console's proxy of EventStore. Cancel by aborting the
// signal: EventStore can't stop a scan, so cancel only drops the answer.
import type { LogPage } from './stream'

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
