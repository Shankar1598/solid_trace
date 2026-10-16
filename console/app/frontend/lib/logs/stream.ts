// The stream of Logs a run shows, newest first: the first page, then older
// pages as the reader scrolls, up to MAX_STREAM_LOGS. A search for rare Logs
// answers after a scan budget with what it has found, maybe nothing, so the
// stream fetches on by itself until it has the page it wants.

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
  /** How long EventStore took to answer, in ms. */
  elapsed_ms?: number
}

export interface Stream {
  logs: Log[]
  /** Where the page of older Logs starts; null once there are none. */
  nextCursor: string | null
  /** How many Logs the stream fetches on to have, while older ones remain. */
  target: number
}

/** Past this many Logs the stream asks to narrow the time range instead. */
export const MAX_STREAM_LOGS = 10_000

/** How many Logs a page holds. */
export const PAGE_LOGS = 200

export function startStream(page: LogPage): Stream {
  return { logs: page.logs.slice(0, MAX_STREAM_LOGS), nextCursor: page.next_cursor, target: PAGE_LOGS }
}

export function addOlderPage(stream: Stream, page: LogPage): Stream {
  return {
    ...stream,
    logs: stream.logs.concat(page.logs).slice(0, MAX_STREAM_LOGS),
    nextCursor: page.next_cursor,
  }
}

/** The stream wanting a page more of older Logs, as when the reader scrolls to its end. */
export function wantOlder(stream: Stream): Stream {
  return { ...stream, target: Math.min(stream.logs.length + PAGE_LOGS, MAX_STREAM_LOGS) }
}

/** Whether the stream fetches older Logs without being asked: it is short of its target and some remain. */
export function keepsFetching(stream: Stream): boolean {
  return stream.nextCursor !== null && stream.logs.length < Math.min(stream.target, MAX_STREAM_LOGS)
}

/**
 * Fetches older pages into the stream while it keepsFetching, calling show
 * with each. A cancelled run's fetchPage rejects, which ends it.
 */
export async function fillStream(
  stream: Stream,
  fetchPage: (cursor: string) => Promise<LogPage>,
  show: (stream: Stream) => void,
): Promise<Stream> {
  while (keepsFetching(stream)) {
    stream = addOlderPage(stream, await fetchPage(stream.nextCursor!))
    show(stream)
  }
  return stream
}

/** more: older Logs can load; end: there are none; capped: too many to show. */
export function streamEnd(stream: Stream): 'more' | 'end' | 'capped' {
  if (stream.nextCursor === null) return 'end'
  return stream.logs.length >= MAX_STREAM_LOGS ? 'capped' : 'more'
}
