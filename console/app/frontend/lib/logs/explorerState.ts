// The Logs explorer's state and its URL. The Project is in the path; the
// search holds the time range as given (relative stays relative, absolute
// as UTC ISO times), the mode, the Query text and, in SQL mode, the SQL text.
// Each run pushes a history entry; typing doesn't, so Back undoes the last
// run. Switching mode replaces the entry's mode, so a run reopens in the mode
// it was left in. SQL never runs on opening a URL, only when Run is pressed,
// so a crafted link can't run SQL as whoever opens it.

export type Mode = 'builder' | 'query' | 'sql'

/** A span back from now, such as `15m`, `1h` or `7d`. */
export type RelativeRange = { kind: 'relative'; span: string }
export type AbsoluteRange = { kind: 'absolute'; from: Date; to: Date }
export type TimeRange = RelativeRange | AbsoluteRange

export interface ExplorerState {
  range: TimeRange
  mode: Mode
  query: string
  /** The SQL text, in SQL mode; empty in the others. */
  sql: string
}

/** What last ran (and is in the URL), and what is being edited. */
export interface Explorer {
  current: ExplorerState
  draft: ExplorerState
}

/** The modes, in the switch's order. */
export const MODES: Mode[] = ['builder', 'query', 'sql']

/** The modes the switch offers: SQL only while the instance has SQL queries on. */
export function modesFor(sqlQueriesEnabled: boolean): Mode[] {
  return sqlQueriesEnabled ? MODES : MODES.filter(m => m !== 'sql')
}
const SPAN = /^(\d+)([mhd])$/
const UNIT_MS = { m: 60_000, h: 3_600_000, d: 86_400_000 } as const

export const DEFAULT_RANGE: RelativeRange = { kind: 'relative', span: '1h' }

/** The relative ranges the time-range select offers. */
export const RANGE_PRESETS: { span: string; label: string }[] = [
  { span: '15m', label: 'Last 15 minutes' },
  { span: '1h', label: 'Last hour' },
  { span: '6h', label: 'Last 6 hours' },
  { span: '24h', label: 'Last 24 hours' },
  { span: '7d', label: 'Last 7 days' },
  { span: '30d', label: 'Last 30 days' },
]

export function spanLabel(span: string): string {
  const preset = RANGE_PRESETS.find(p => p.span === span)
  if (preset) return preset.label
  const match = SPAN.exec(span)
  if (!match) return span
  const unit = { m: 'minute', h: 'hour', d: 'day' }[match[2] as 'm' | 'h' | 'd']
  return `Last ${match[1]} ${unit}${match[1] === '1' ? '' : 's'}`
}

/** A URL's state; a mode not in `modes` (SQL while it's off) opens the Builder. */
export function stateFromSearch(search: string, modes: Mode[] = MODES): ExplorerState {
  const params = new URLSearchParams(search)
  const given = params.get('mode') as Mode | null
  const mode = given && modes.includes(given) ? given : 'builder'
  return {
    range: rangeFromParams(params),
    mode,
    query: params.get('q') ?? '',
    sql: mode === 'sql' ? params.get('sql') ?? '' : '',
  }
}

export function searchOf(state: ExplorerState): string {
  const params = new URLSearchParams()
  if (state.range.kind === 'relative') {
    params.set('range', state.range.span)
  } else {
    params.set('from', state.range.from.toISOString())
    params.set('to', state.range.to.toISOString())
  }
  params.set('mode', state.mode)
  if (state.query !== '') params.set('q', state.query)
  if (state.sql !== '') params.set('sql', state.sql)
  return `?${params}`
}

/** The times a range covers at `now`, `[from, to)`. */
export function resolveRange(range: TimeRange, now: Date): { from: Date; to: Date } {
  if (range.kind === 'absolute') return { from: range.from, to: range.to }
  return { from: new Date(now.getTime() - spanMs(range.span)), to: now }
}

export function openExplorer(search: string, modes: Mode[] = MODES): Explorer {
  const state = stateFromSearch(search, modes)
  return { current: state, draft: state }
}

/** Whether opening the explorer runs what its URL holds: never SQL. */
export function runsOnOpen(explorer: Explorer): boolean {
  return explorer.current.mode !== 'sql'
}

export function typeQuery(explorer: Explorer, query: string): Explorer {
  return { ...explorer, draft: { ...explorer.draft, query } }
}

export function typeSql(explorer: Explorer, sql: string): Explorer {
  return { ...explorer, draft: { ...explorer.draft, sql } }
}

export function pickRange(explorer: Explorer, range: TimeRange): Explorer {
  return { ...explorer, draft: { ...explorer.draft, range } }
}

/**
 * Switches mode without running: what ran and what is being edited stay, and
 * `replace` is the search to put in place of the current history entry's.
 */
export function switchMode(explorer: Explorer, mode: Mode): { explorer: Explorer; replace: string } {
  const current = { ...explorer.current, mode }
  return { explorer: { current, draft: { ...explorer.draft, mode } }, replace: searchOf(current) }
}

/**
 * Query mode writes its stages after the Query's Project, so an empty Query
 * shows there as `projectQuery`, the Project's slug alone. That reads the
 * same Logs as the empty Query, so it goes in what ran as well as the draft:
 * it isn't an edit, and neither running it again nor switching back to the
 * Builder adds a history entry or runs anything new.
 */
export function fillEmptyQuery(explorer: Explorer, projectQuery: string): Explorer {
  const fill = (state: ExplorerState) => (state.mode === 'query' && state.query === '' ? { ...state, query: projectQuery } : state)
  return { current: fill(explorer.current), draft: fill(explorer.draft) }
}

/**
 * Runs the draft. `push` is the search to push as a new history entry, or
 * null when the run changes nothing in the URL (running it again only
 * refreshes the Logs).
 */
export function runExplorer(explorer: Explorer): { explorer: Explorer; push: string | null } {
  const search = searchOf(explorer.draft)
  const push = search === searchOf(explorer.current) ? null : search
  return { explorer: { current: explorer.draft, draft: explorer.draft }, push }
}

function rangeFromParams(params: URLSearchParams): TimeRange {
  const span = params.get('range')
  if (span !== null) return SPAN.test(span) && spanMs(span) > 0 ? { kind: 'relative', span } : DEFAULT_RANGE

  const from = new Date(params.get('from') ?? NaN)
  const to = new Date(params.get('to') ?? NaN)
  if (isNaN(from.getTime()) || isNaN(to.getTime()) || from > to) return DEFAULT_RANGE
  return { kind: 'absolute', from, to }
}

function spanMs(span: string): number {
  const match = SPAN.exec(span)
  if (!match) return 0
  return Number(match[1]) * UNIT_MS[match[2] as keyof typeof UNIT_MS]
}
