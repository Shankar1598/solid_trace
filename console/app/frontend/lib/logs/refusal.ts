// EventStore's refusal of an explorer run: an answer, not an error. A Query
// that matches text and would read more than EventStore's budget isn't run;
// the answer says how much it would read and how to read less. A busy
// refusal says another large Query is running, and no more.
import type { AbsoluteRange } from '../timeRange'

/** How much a run of a Query would read, by EventStore's counts. */
export interface ReadEstimate {
  from: string
  to: string
  text_match: boolean
  logs: number
  /** Kept bytes the Query would read. */
  bytes: number
  /** The most a Query that matches text may read; 0 when nothing is refused. */
  budget_bytes: number
  over_budget: boolean
  heavy: boolean
  forced: boolean
}

/** A range of time, and the bytes the Query would read in it. */
export interface ReadRange {
  from: string
  to: string
  bytes: number
}

/** One Source's Logs and kept bytes in the range. */
export interface SourceRead {
  service: string
  /** Empty for Logs with no file. */
  file: string
  logs: number
  bytes: number
}

/**
 * Why a run wasn't run. Only message is sure: a busy refusal may carry
 * nothing else, and an older EventStore no busy.
 */
export interface Refusal {
  message: string
  /** Another large Query is running: Run anyway would be refused the same way. */
  busy?: boolean
  estimate?: ReadEstimate | null
  /** The longest range ending where the run's does that fits, or null. */
  largest_range?: ReadRange | null
  /** The range's busiest Sources the Query's filters keep. */
  top_files?: SourceRead[] | null
  narrower_query?: { query: string; from: string; to: string } | null
}

/** An answer's refusal, or null when it isn't one. */
export function refusalOf(body: unknown): Refusal | null {
  const refusal = (body as { refusal?: unknown } | null)?.refusal
  if (typeof refusal !== 'object' || refusal === null) return null
  return typeof (refusal as Refusal).message === 'string' ? (refusal as Refusal) : null
}

/** What the explorer shows of a refusal, in place of results; a part it can't show is null or empty. */
export interface RefusalView {
  /** Why: the message's first line when the parts below show the rest, else all of it. */
  message: string
  /** The read against the budget; budgetShare is the budget's part of the read, for a meter. */
  estimate: { bytes: string; logs: string; budget: string; budgetShare: number } | null
  /** The largest range that fits, to run instead, its length and what it would read. */
  range: { range: AbsoluteRange; length: string; bytes: string } | null
  /** The busiest files, each to filter on; file is null for Logs with no file, which no filter keeps. */
  files: { service: string; file: string | null; logs: string; bytes: string }[]
  /** Whether Run anyway is offered: not while another large Query runs, as a forced run would be refused the same way. */
  runAnyway: boolean
}

export function refusalView(refusal: Refusal): RefusalView {
  const estimate = !refusal.busy && refusal.estimate && refusal.estimate.budget_bytes > 0 ? refusal.estimate : null
  const largest = refusal.busy ? null : refusal.largest_range ?? null
  const files = refusal.busy ? [] : refusal.top_files ?? []
  const parts = estimate !== null || largest !== null || files.length > 0
  return {
    message: parts ? refusal.message.split('\n')[0] : refusal.message,
    estimate: estimate && {
      bytes: sizeText(estimate.bytes),
      logs: `${countText(estimate.logs)} Logs`,
      budget: sizeText(estimate.budget_bytes),
      budgetShare: estimate.bytes > 0 ? Math.min(1, estimate.budget_bytes / estimate.bytes) : 1,
    },
    range: largest && {
      range: { kind: 'absolute', from: new Date(largest.from), to: new Date(largest.to) },
      length: lengthText(Date.parse(largest.to) - Date.parse(largest.from)),
      bytes: sizeText(largest.bytes),
    },
    files: files.map(f => ({ service: f.service, file: f.file === '' ? null : f.file, logs: `${countText(f.logs)} Logs`, bytes: sizeText(f.bytes) })),
    runAnyway: !refusal.busy,
  }
}

/** Bytes in GiB, MiB or KiB to a tenth, rounded down, as EventStore's message writes them. */
function sizeText(bytes: number): string {
  for (const [size, unit] of [[2 ** 30, 'GiB'], [2 ** 20, 'MiB'], [2 ** 10, 'KiB']] as const) {
    if (bytes >= size) return `${tenths(bytes / size)} ${unit}`
  }
  return `${bytes} bytes`
}

/** A count in billions, millions or thousands to a tenth, rounded down, as EventStore writes it. */
function countText(n: number): string {
  for (const [size, unit] of [[1e9, 'B'], [1e6, 'M'], [1e3, 'k']] as const) {
    if (n >= size) return `${tenths(n / size)}${unit}`
  }
  return String(n)
}

function tenths(x: number): string {
  return (Math.floor(x * 10) / 10).toFixed(1).replace(/\.0$/, '')
}

/** A range's length in days and hours, or hours and minutes, as EventStore writes it: 2d 1h, 7h 12m, 45m. */
function lengthText(ms: number): string {
  const minutes = Math.floor(ms / 60_000)
  const days = Math.floor(minutes / 1440)
  const hours = Math.floor((minutes % 1440) / 60)
  const rest = minutes % 60
  if (days > 0) return hours > 0 ? `${days}d ${hours}h` : `${days}d`
  if (hours > 0) return rest > 0 ? `${hours}h ${rest}m` : `${hours}h`
  return `${Math.max(rest, 1)}m`
}
