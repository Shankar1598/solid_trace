// The stream of Logs a run shows, newest first: the first page, then older
// pages as the reader scrolls, up to MAX_STREAM_LOGS.

export type AttributeValue = string | number | boolean | null | object

/** A Log as EventStore answers it. */
export interface Log {
  id: string
  /** RFC 3339, UTC. */
  time: string
  level?: string
  message: string
  service?: string
  environment?: string
  trace_id?: string
  span_id?: string
  attributes: Record<string, AttributeValue>
}

export interface LogPage {
  logs: Log[]
  next_cursor: string | null
}

export interface Stream {
  logs: Log[]
  /** Where the page of older Logs starts; null once there are none. */
  nextCursor: string | null
}

/** Past this many Logs the stream asks to narrow the time range instead. */
export const MAX_STREAM_LOGS = 10_000

export function startStream(page: LogPage): Stream {
  return { logs: page.logs.slice(0, MAX_STREAM_LOGS), nextCursor: page.next_cursor }
}

export function addOlderPage(stream: Stream, page: LogPage): Stream {
  return {
    logs: stream.logs.concat(page.logs).slice(0, MAX_STREAM_LOGS),
    nextCursor: page.next_cursor,
  }
}

/** more: older Logs can load; end: there are none; capped: too many to show. */
export function streamEnd(stream: Stream): 'more' | 'end' | 'capped' {
  if (stream.nextCursor === null) return 'end'
  return stream.logs.length >= MAX_STREAM_LOGS ? 'capped' : 'more'
}
