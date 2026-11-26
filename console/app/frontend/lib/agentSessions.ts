// The Agent sessions page: its search (the range, the filters and the
// order stay in the URL, so that a filtered view can be shared), and how
// its numbers read. The rows and totals come from AgentSessionsView.
import { formatDistanceStrict } from 'date-fns'
import { EMPTY_BUILDER, queryOf } from './logs/builder'
import { searchOf } from './logs/explorerState'
import type { TimeRange } from './timeRange'

export type RunFilter = 'run' | 'none'

export const agentSessionPath = (slug: string, id: string) => `/${slug}/agent_sessions/${id}`

export interface AgentSessionFilters {
  /** A user id. */
  person: number | null
  /** host/workspace/name. */
  repository: string | null
  /** The slug of a Project of Runs. */
  project: string | null
  run: RunFilter | null
}

/** The columns that EventStore sorts the list by. */
export type AgentSort = 'last' | 'start' | 'tokens' | 'cost'
export type SortDirection = 'desc' | 'asc'

/** The order of the list, as the URL names it; null is the default of each. */
export interface AgentSessionOrder {
  sort: AgentSort | null
  direction: SortDirection | null
}

/** The order of the list when the URL names none: the latest activity first. */
export const DEFAULT_ORDER = { sort: 'last', direction: 'desc' } as const satisfies { sort: AgentSort; direction: SortDirection }

/** The order that the list has: the URL's, with the default for each part that it leaves out. */
export function orderOf(order: AgentSessionOrder): { sort: AgentSort; direction: SortDirection } {
  return { sort: order.sort ?? DEFAULT_ORDER.sort, direction: order.direction ?? DEFAULT_ORDER.direction }
}

/**
 * The order after a click on a column's heading: the other direction on
 * the sorted column, else that column descending (the latest times, the
 * most tokens, the highest cost first).
 */
export function nextOrder(order: AgentSessionOrder, sort: AgentSort): { sort: AgentSort; direction: SortDirection } {
  const now = orderOf(order)
  if (now.sort === sort) return { sort, direction: now.direction === 'desc' ? 'asc' : 'desc' }
  return { sort, direction: 'desc' }
}

const ORDER_TEXT: Record<AgentSort, Record<SortDirection, string>> = {
  last: { desc: 'latest activity first', asc: 'earliest activity first' },
  start: { desc: 'latest start first', asc: 'earliest start first' },
  tokens: { desc: 'most tokens first', asc: 'fewest tokens first' },
  cost: { desc: 'highest cost first', asc: 'lowest cost first' },
}

/** The order in words, as "most tokens first". */
export function orderText(order: AgentSessionOrder): string {
  const { sort, direction } = orderOf(order)
  return ORDER_TEXT[sort][direction]
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
  /** The effort of the reply whose model is model, as high; "" for none. */
  effort: string
  /** The last /rename, else Claude Code's title, else the start of the first person message; "" for none. */
  title: string
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

/**
 * The page's search for a range, filters and an order: a span stays a
 * span, a filter that is not set is left out, and so is the default order.
 */
export function agentSessionsSearch(range: TimeRange, filters: AgentSessionFilters, order: AgentSessionOrder = { sort: null, direction: null }): string {
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
  const { sort, direction } = orderOf(order)
  if (sort !== DEFAULT_ORDER.sort || direction !== DEFAULT_ORDER.direction) {
    params.set('sort', sort)
    params.set('direction', direction)
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

/**
 * A model with the effort of its reply, as "claude-opus-5-5 · high"; the model alone when there is no effort.
 * A `<synthetic>` reply is Claude Code's own, as an API error, so it has no model text.
 */
export function modelText(model: string | undefined, effort: string | undefined): string {
  if (model === '<synthetic>') return ''
  return [model, effort].filter(Boolean).join(' · ')
}

/** An estimated cost in dollars. A cost under a cent is "<$0.01"; no cost is never $0. */
export function costText(usd: number | null): string {
  if (usd === null) return 'No cost'
  if (usd > 0 && usd < 0.005) return '<$0.01'
  return dollars.format(usd)
}

/**
 * How long ago a time was, as the list shows the last activity: "a few
 * seconds ago" in its first minute, then "5 minutes ago", "3 hours ago",
 * "2 days ago". A time a little after now (another clock) is "a few
 * seconds ago" too.
 */
export function agoText(time: string, now: Date = new Date()): string {
  const seconds = Math.max(0, (now.getTime() - Date.parse(time)) / 1000)
  if (seconds < 60) return 'a few seconds ago'
  return `${formatDistanceStrict(new Date(time), now, { roundingMethod: 'floor' })} ago`
}

/** The tokens of each kind, added. */
export function totalTokens(t: Pick<AgentSessionRow, 'input_tokens' | 'output_tokens' | 'cache_read_tokens' | 'cache_write_tokens'>): number {
  return t.input_tokens + t.output_tokens + t.cache_read_tokens + t.cache_write_tokens
}

/**
 * The title of an Agent session as the pages show it: its title, or, when
 * it has none, its short id, which the pages show muted.
 */
export function titleOf({ id, title }: Pick<AgentSessionRow, 'id' | 'title'>): { text: string; untitled: boolean } {
  const text = title?.trim() ?? ''
  return text ? { text, untitled: false } : { text: id.slice(0, 8), untitled: true }
}

/** The command that continues an Agent session on another machine. */
export function resumeCommand(id: string): string {
  return `solidtrace resume ${id}`
}

/**
 * What the resume command does for the viewer: the viewer's own Agent
 * session continues with the same id; another person's, or a Run's,
 * continues as a new Agent session of the viewer's.
 */
export function resumeText({ person, run }: Pick<AgentSessionRow, 'person' | 'run'>, viewerId: number | undefined): string {
  const where = 'Run this command in a checkout of the repository of this Agent session.'
  if (!run && person && viewerId !== undefined && Number(person.id) === viewerId) {
    return `${where} It continues this Agent session with the same id. Close it on the other machine first.`
  }
  return `${where} It continues as a new Agent session of yours, and this Agent session does not change.`
}

/** Whose an Agent session is: the person, or the Run with its Project. */
export function whoText({ person, run }: Pick<AgentSessionRow, 'person' | 'run'>): string {
  if (run) return run.project ? `Run ${run.id} (${run.project})` : `Run ${run.id}`
  return person?.name ?? 'Unknown'
}
