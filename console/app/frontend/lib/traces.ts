// A trace as Console's TraceView answers it for the trace page.
import { valueText } from '@/lib/charts/axis'

export interface TraceSpan {
  span_id: string
  parent_span_id: string
  segment_id: string
  is_segment: boolean
  transaction: string
  op: string
  description: string
  group: string
  repeats: number
  status: string
  start: string
  duration_us: number
  self_time_us: number
  sample_rate: number
  environment: string
  release: string
  server_name: string
  attributes: Record<string, string>
  project: TraceProject
  /** How deep it is under the trace's first Span. */
  depth: number
  /** Its start from the trace's, in microseconds. */
  offset_us: number
  /**
   * How many sibling Spans of one group the row is, a query repeated in a
   * loop; duration_us and self_time_us are then their totals.
   */
  count: number
  /** From its first Span's start to its last's end, in microseconds. */
  span_us: number
  /** Whether it's on the critical path: the Spans the trace's end waited on. */
  critical: boolean
}

export interface TraceProject {
  id: number
  name: string
  slug: string
}

export interface TraceLog {
  id: string
  time: string
  level?: string
  service?: string
  message: string
  project: TraceProject
  /** The explorer over the Log's second, among the trace's Logs. */
  url: string
}

export interface TraceIssue {
  id: number
  number: number
  title: string
  status: string
  project: TraceProject
  /** How many of its Events are in the trace. */
  events: number
  /** The Issue's page on its first Event in the trace. */
  url: string
}

export interface Trace {
  trace_id: string
  /** The time the link gave, for a trace that wasn't sampled. */
  time: string | null
  event_store: { available: boolean; error?: string }
  /** Whether it has Spans; null when EventStore didn't answer. */
  sampled: boolean | null
  duration_us: number
  span_count: number
  spans: TraceSpan[]
  logs: TraceLog[]
  more_logs: boolean
  /** Each Project's Logs of the trace in the explorer. */
  logs_links: { project: TraceProject; url: string }[]
  issues: TraceIssue[]
}

/** A trace id as Sentry SDKs send it: 32 hex digits. */
export const TRACE_ID = /^[0-9a-f]{32}$/i

/** A time of day in the reader's zone to the millisecond, as "11:00:00.050". */
export function clockTime(time: string): string {
  const date = new Date(time)
  if (isNaN(date.getTime())) return time
  return date.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit', second: '2-digit', fractionalSecondDigits: 3, hourCycle: 'h23' })
}

/** The trace page of a trace id, at a time (an Event's or a Log's) for a trace that wasn't sampled. */
export function tracePath(orgSlug: string, traceId: string, time?: string | null): string {
  const path = `/${orgSlug}/traces/${traceId.toLowerCase()}`
  return time ? `${path}?${new URLSearchParams({ time })}` : path
}

/** A duration in microseconds, in ms to two significant places below 10 ms, else whole ms or s. */
export function durationText(us: number): string {
  const ms = us / 1000
  if (ms >= 10_000) return `${(ms / 1000).toFixed(1)} s`
  if (ms >= 10) return `${Math.round(ms)} ms`
  return `${Number(ms.toPrecision(2))} ms`
}

/** A duration in microseconds in full, for a title. */
export function microsecondsText(us: number): string {
  return `${valueText(us)} µs`
}
