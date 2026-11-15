// The Agent sessions page: its search (the range and the filters stay in
// the URL, so that a filtered view can be shared), and how its numbers
// read. The rows and totals come from AgentSessionsView.
import { EMPTY_BUILDER, queryOf } from './logs/builder'
import { searchOf } from './logs/explorerState'
import type { TimeRange } from './timeRange'

export type RunFilter = 'run' | 'none'

export interface AgentSessionFilters {
  /** A user id. */
  person: number | null
  /** host/workspace/name. */
  repository: string | null
  /** The slug of a Project of Runs. */
  project: string | null
  run: RunFilter | null
}

export interface AgentSessionRow {
  id: string
  first: string
  last: string
  person: { id: number; name: string } | null
  run: { id: string; project: string | null } | null
  harness: string
  repository: string
  folder: string
  parent_session_id: string
  model: string
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  /** The estimate at list prices; null is no cost. */
  cost_usd: number | null
  person_messages: number
  turns: number
  tool_calls: number
  subagents: number
  flags: string[]
}

export interface AgentSessionTotals {
  agent_sessions: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cost_usd: number | null
  no_cost: number
}

/** The page's search for a range and filters: a span stays a span, and a filter that is not set is left out. */
export function agentSessionsSearch(range: TimeRange, filters: AgentSessionFilters): string {
  const params = new URLSearchParams()
  if (range.kind === 'relative') {
    params.set('range', range.span)
  } else {
    params.set('from', range.from.toISOString())
    params.set('to', range.to.toISOString())
  }
  for (const name of ['person', 'repository', 'project', 'run'] as const) {
    const value = filters[name]
    if (value !== null && value !== '') params.set(name, String(value))
  }
  return `?${params}`
}

/** Where search across Agent sessions goes: the Agent Project's Logs explorer, over range. */
export interface SearchAcross {
  logs_path: string
  /** The Agent Project's slug. */
  project: string
  /** A span, as 7d. */
  range: string
}

/**
 * Search across Agent sessions: the Logs explorer on the Agent Project,
 * with the text as the Builder's search, over the range given (7 days).
 * A member can make the range longer there.
 */
export function searchAcrossHref(search: SearchAcross, text: string): string {
  const query = queryOf({ ...EMPTY_BUILDER, search: text }, search.project)
  return search.logs_path + searchOf({ range: { kind: 'relative', span: search.range }, mode: 'builder', query, sql: '', viz: 'auto' })
}

const shortNumber =new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 })
const dollars = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 2 })

/** A count of tokens, short: 12.3K, 4M. */
export function tokensText(tokens: number): string {
  return shortNumber.format(tokens)
}

/** An estimated cost in dollars. A cost under a cent is "<$0.01"; no cost is never $0. */
export function costText(usd: number | null): string {
  if (usd === null) return 'No cost'
  if (usd > 0 && usd < 0.005) return '<$0.01'
  return dollars.format(usd)
}

/** The tokens of each kind, added. */
export function totalTokens(t: Pick<AgentSessionRow, 'input_tokens' | 'output_tokens' | 'cache_read_tokens' | 'cache_write_tokens'>): number {
  return t.input_tokens + t.output_tokens + t.cache_read_tokens + t.cache_write_tokens
}

/** Whose an Agent session is: the person, or the Run with its Project. */
export function whoText({ person, run }: Pick<AgentSessionRow, 'person' | 'run'>): string {
  if (run) return run.project ? `Run ${run.id} (${run.project})` : `Run ${run.id}`
  return person?.name ?? 'Unknown'
}
