// The Logs explorer's state and its URL. The Project is in the path; the
// search holds the time range as given (relative stays relative, absolute
// as UTC ISO times), the mode and the Query text. Each run pushes a history
// entry; typing doesn't, so Back undoes the last run.

export type Mode = 'builder' | 'query'

/** A span back from now, such as `15m`, `1h` or `7d`. */
export type RelativeRange = { kind: 'relative'; span: string }
export type AbsoluteRange = { kind: 'absolute'; from: Date; to: Date }
export type TimeRange = RelativeRange | AbsoluteRange

export interface ExplorerState {
  range: TimeRange
  mode: Mode
  query: string
}

/** What last ran (and is in the URL), and what is being edited. */
export interface Explorer {
  current: ExplorerState
  draft: ExplorerState
}

const MODES: Mode[] = ['builder', 'query']
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

export function stateFromSearch(search: string): ExplorerState {
  const params = new URLSearchParams(search)
  const mode = params.get('mode') as Mode | null
  return {
    range: rangeFromParams(params),
    mode: mode && MODES.includes(mode) ? mode : 'builder',
    query: params.get('q') ?? '',
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
  return `?${params}`
}

/** The times a range covers at `now`, `[from, to)`. */
export function resolveRange(range: TimeRange, now: Date): { from: Date; to: Date } {
  if (range.kind === 'absolute') return { from: range.from, to: range.to }
  return { from: new Date(now.getTime() - spanMs(range.span)), to: now }
}

export function openExplorer(search: string): Explorer {
  const state = stateFromSearch(search)
  return { current: state, draft: state }
}

export function typeQuery(explorer: Explorer, query: string): Explorer {
  return { ...explorer, draft: { ...explorer.draft, query } }
}

export function pickRange(explorer: Explorer, range: TimeRange): Explorer {
  return { ...explorer, draft: { ...explorer.draft, range } }
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
