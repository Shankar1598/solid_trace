// A trace as Console's TraceView answers it for the trace page.

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
  project: { id: number; name: string; slug: string }
  /** How deep it is under the trace's first Span. */
  depth: number
  /** Its start from the trace's, in microseconds. */
  offset_us: number
}

export interface Trace {
  trace_id: string
  event_store: { available: boolean; error?: string }
  duration_us: number
  spans: TraceSpan[]
}

/** A trace id as Sentry SDKs send it: 32 hex digits. */
export const TRACE_ID = /^[0-9a-f]{32}$/i

/** A duration in microseconds, in ms to two significant places below 10 ms, else whole ms or s. */
export function durationText(us: number): string {
  const ms = us / 1000
  if (ms >= 10_000) return `${(ms / 1000).toFixed(1)} s`
  if (ms >= 10) return `${Math.round(ms)} ms`
  return `${Number(ms.toPrecision(2))} ms`
}
