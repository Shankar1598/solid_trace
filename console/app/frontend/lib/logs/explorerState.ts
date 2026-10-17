// The Logs explorer's state and its URL. The Project is in the path; the
// search holds the time range as given (relative stays relative, absolute
// as UTC ISO times), the mode, the Query text, in SQL mode the SQL text, and
// the chart a table is drawn as. Each run pushes a history entry; typing
// doesn't, so Back undoes the last run. Switching mode or chart replaces the
// entry's, so a run reopens as it was left. SQL never runs on opening a URL,
// only when Run is pressed, so a crafted link can't run SQL as whoever opens it.
import { isSpan, type RelativeRange, type TimeRange } from '../timeRange'

export { resolveRange, type AbsoluteRange, type RelativeRange, type TimeRange } from '../timeRange'

export type Mode = 'builder' | 'query' | 'sql'

/**
 * How a table that can be charted is shown: as a line, area or bar chart over
 * the table, or as the table alone. auto draws the chart its shape suggests
 * and leaves no trace in the URL.
 */
export type Viz = 'auto' | 'line' | 'area' | 'bars' | 'table'
const VIZES: Viz[] = ['auto', 'line', 'area', 'bars', 'table']

export interface ExplorerState {
  range: TimeRange
  mode: Mode
  query: string
  /** The SQL text, in SQL mode; empty in the others. */
  sql: string
  viz: Viz
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

export const DEFAULT_RANGE: RelativeRange = { kind: 'relative', span: '1h' }

/** A URL's state; a mode not in `modes` (SQL while it's off) opens the Builder. */
export function stateFromSearch(search: string, modes: Mode[] = MODES): ExplorerState {
  const params = new URLSearchParams(search)
  const given = params.get('mode') as Mode | null
  const mode = given && modes.includes(given) ? given : 'builder'
  const viz = params.get('viz') as Viz | null
  return {
    range: rangeFromParams(params),
    mode,
    query: params.get('q') ?? '',
    sql: mode === 'sql' ? params.get('sql') ?? '' : '',
    viz: viz && VIZES.includes(viz) ? viz : 'auto',
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
  if (state.viz !== 'auto') params.set('viz', state.viz)
  return `?${params}`
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
 * Switches mode without running: what ran stays, and so does what is being
 * edited, except in the Builder (see draftIn). `replace` is the search to put
 * in place of the current history entry's.
 */
export function switchMode(explorer: Explorer, mode: Mode): { explorer: Explorer; replace: string } {
  const current = { ...explorer.current, mode }
  return { explorer: { current, draft: draftIn(explorer.draft, current) }, replace: searchOf(current) }
}

/**
 * The draft once the explorer is in `current`'s mode without a run. The
 * Builder shows what ran, so there the draft's Query goes back to it: a Query
 * typed in Query mode that never ran must not run with the next Run, range
 * pick or zoom under a Builder that doesn't show it.
 */
export function draftIn(draft: ExplorerState, current: ExplorerState): ExplorerState {
  const { mode } = current
  return mode === 'builder' ? { ...draft, mode, query: current.query } : { ...draft, mode }
}

/**
 * The Project's Logs unfiltered in the Builder, over the range shown when it
 * reaches now, or else DEFAULT_RANGE: where a Log just sent shows. Run it.
 */
export function allLogsNow(explorer: Explorer, projectQuery: string): Explorer {
  const { explorer: inBuilder } = switchMode(explorer, 'builder')
  const range = explorer.current.range.kind === 'relative' ? explorer.current.range : DEFAULT_RANGE
  return { ...inBuilder, draft: { ...inBuilder.draft, query: projectQuery, range } }
}

/**
 * Draws tables another way, without running: what ran and the draft both
 * change, and `replace` is the search to put in place of the current history
 * entry's.
 */
export function chooseViz(explorer: Explorer, viz: Viz): { explorer: Explorer; replace: string } {
  const current = { ...explorer.current, viz }
  return { explorer: { current, draft: { ...explorer.draft, viz } }, replace: searchOf(current) }
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

/**
 * How Copy link writes the range: as shown turns a relative range into the
 * absolute one the run used, so the link shows the same Logs later; live
 * keeps it relative, so it shows the Logs of when it is opened.
 */
export type LinkKind = 'shown' | 'live'

/** The search of a link to the explorer in `state`, which ran over `ran`. */
export function linkSearch(state: ExplorerState, kind: LinkKind, ran: { from: Date; to: Date }): string {
  if (kind === 'live' || state.range.kind === 'absolute') return searchOf(state)
  return searchOf({ ...state, range: { kind: 'absolute', from: ran.from, to: ran.to } })
}

function rangeFromParams(params: URLSearchParams): TimeRange {
  const span = params.get('range')
  if (span !== null) return isSpan(span) ? { kind: 'relative', span } : DEFAULT_RANGE

  const from = new Date(params.get('from') ?? NaN)
  const to = new Date(params.get('to') ?? NaN)
  if (isNaN(from.getTime()) || isNaN(to.getTime()) || from > to) return DEFAULT_RANGE
  return { kind: 'absolute', from, to }
}
