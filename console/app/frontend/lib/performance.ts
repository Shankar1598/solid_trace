// A Project's Performance and a Transaction's page, as Console's
// TransactionsView and TransactionView answer them, and how their URLs hold
// a range and an environment.
import { valueText } from '@/lib/charts/axis'
import type { TimeRange } from '@/lib/timeRange'

export interface ProjectRef { id: number; name: string; slug: string }

/** A range as the pages answer it: a span back from now, or null, and the times it covered. */
export interface AnsweredRange { span: string | null; from: string; to: string }

export interface EventStoreState { available: boolean; error?: string }

/** A Transaction's numbers over a period. Throughput and time consumed are estimates, weighted by sample rate. */
export interface TransactionNumbers {
  sampled: number
  throughput_per_minute: number
  p50_us: number
  p95_us: number
  p99_us: number
  error_rate: number
  time_consumed_us: number
}

export type NumberKey = Exclude<keyof TransactionNumbers, 'sampled'>

export interface TransactionRow extends TransactionNumbers {
  name: string
  label: string
  previous: TransactionNumbers | null
  /** Each number's change: a share of the previous number (null from 0), the error rate's a difference. */
  change: Record<NumberKey, number | null> | null
}

export interface Performance {
  project: ProjectRef
  range: AnsweredRange
  environment: string | null
  environments: string[]
  event_store: EventStoreState
  transactions: TransactionRow[]
}

export interface ReleaseDurations {
  release: string
  sampled: number
  p50_us: number
  p95_us: number
  points: { time: string; sampled: number; p50_us: number; p95_us: number }[]
}

export interface SelfTime { op: string; share: number; avg_self_time_us: number }

export interface GroupSelfTime extends SelfTime { group: string; description: string; normalised: string }

export interface RepeatedGroup {
  group: string
  op: string
  description: string
  normalised: string
  median: number
  max: number
  sampled: number
}

export interface SlowTrace {
  trace_id: string
  start: string
  duration_us: number
  status: string
  release: string
  environment: string
}

export interface Transaction {
  project: ProjectRef
  name: string
  label: string
  range: AnsweredRange
  environment: string | null
  event_store: EventStoreState
  summary: TransactionNumbers | null
  bucket_seconds: number | null
  releases: ReleaseDurations[]
  ops: SelfTime[]
  groups: GroupSelfTime[]
  repeats: RepeatedGroup[]
  slowest: SlowTrace[]
}

export type SortKey = 'label' | NumberKey
export interface Sort { key: SortKey; descending: boolean }

/** The sort Performance opens with: the most time consuming first. */
export const DEFAULT_SORT: Sort = { key: 'time_consumed_us', descending: true }

/** Rows sorted by a column, names A to Z where numbers tie. */
export function sortTransactions(rows: TransactionRow[], sort: Sort): TransactionRow[] {
  const sign = sort.descending ? -1 : 1
  return [...rows].sort((a, b) => {
    const by = sort.key === 'label' ? a.label.localeCompare(b.label) : a[sort.key] - b[sort.key]
    return by * sign || a.label.localeCompare(b.label)
  })
}

/** The sort after a column's header is clicked: the other way on the same column, else numbers largest first and names A to Z. */
export function nextSort(sort: Sort, key: SortKey): Sort {
  if (sort.key === key) return { key, descending: !sort.descending }
  return { key, descending: key !== 'label' }
}

/** A number's change as a label, such as `+25%` or `−3.1 pts` for the error rate; null when there is none to show. */
export function changeText(key: NumberKey, change: number | null | undefined): string | null {
  if (change === null || change === undefined) return null
  // A share, or the error rate's difference, in percent or points.
  const value = change * 100
  if (Math.abs(value) < 0.05) return '±0'
  const sign = value > 0 ? '+' : '−'
  const size = Math.abs(value)
  const digits = size >= 10 ? 0 : 1
  return key === 'error_rate' ? `${sign}${size.toFixed(digits)} pts` : `${sign}${size.toFixed(digits)}%`
}

/** Whether a change is for the worse: slower, more errors, more time consumed. More throughput is neither. */
export function worse(key: NumberKey, change: number | null | undefined): boolean {
  return key !== 'throughput_per_minute' && (change ?? 0) > 0
}

/** Requests a minute, as an estimate reads: two significant places below 10. */
export function throughputText(perMinute: number): string {
  if (perMinute === 0) return '0'
  return valueText(perMinute >= 10 ? Math.round(perMinute) : Number(perMinute.toPrecision(2)))
}

/** A share as a percent: whole from 10%, else to one decimal. */
export function percentText(share: number): string {
  const value = share * 100
  return `${valueText(value >= 10 || value === 0 ? Math.round(value) : Number(value.toFixed(1)))}%`
}

/** A share in full, for a title. */
export function exactPercentText(share: number): string {
  return `${valueText(share * 100)}%`
}

/** The range a page answered, as the picker holds it. */
export function rangeOfAnswer(range: AnsweredRange): TimeRange {
  if (range.span) return { kind: 'relative', span: range.span }
  return { kind: 'absolute', from: new Date(range.from), to: new Date(range.to) }
}

/** A page's search for a range and environment, with any more params: relative stays relative. */
export function performanceSearch(range: TimeRange, environment: string | null, more: Record<string, string> = {}): string {
  const params = new URLSearchParams(more)
  if (range.kind === 'relative') {
    params.set('range', range.span)
  } else {
    params.set('from', range.from.toISOString())
    params.set('to', range.to.toISOString())
  }
  if (environment) params.set('environment', environment)
  return `?${params}`
}

export function performancePath(org: string, project: string): string {
  return `/${org}/projects/${project}/performance`
}

/** A Transaction's page, over the range and environment Performance shows. */
export function transactionHref(org: string, project: string, name: string, range: TimeRange, environment: string | null): string {
  return `${performancePath(org, project)}/transaction${performanceSearch(range, environment, { name })}`
}
