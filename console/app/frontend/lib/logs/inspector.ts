// What the inspector shows of one Log: its Fields, each a tag, and its JSON,
// with the raw time in UTC so it can be shared exactly.
import type { AbsoluteRange } from '../timeRange'
import { EMPTY_BUILDER, type Builder, type Chip } from './builder'
import { MAX_STREAM_LOGS, PAGE_LOGS, streamEnd, type AttributeValue, type Log, type Stream } from './stream'

export interface InspectorField {
  name: string
  value: AttributeValue
}

// The built-in fields besides time, in the order the inspector lists them.
const BUILT_IN = ['level', 'service', 'environment', 'message', 'trace_id', 'span_id'] as const

/** The built-in fields the Log has, then its Attributes sorted by name. */
export function inspectorFields(log: Log): InspectorField[] {
  const builtIn = BUILT_IN.filter(name => log[name] !== undefined && log[name] !== '').map(name => ({ name, value: log[name] as string }))
  const attributes = Object.keys(log.attributes ?? {}).sort().map(name => ({ name, value: log.attributes[name] }))
  return [...builtIn, ...attributes]
}

/**
 * A Log's time as a UTC ISO timestamp. One already in UTC is kept as written,
 * so digits past the millisecond survive.
 */
export function utcTime(time: string): string {
  if (time.endsWith('Z')) return time
  const date = new Date(time)
  return isNaN(date.getTime()) ? time : date.toISOString()
}

export function inspectorJson(log: Log): string {
  return JSON.stringify({ ...log, time: utcTime(log.time) }, null, 2)
}

/** A Log's time in the reader's zone, as "Oct 3, 2026 at 19:40:46.319 BST". */
export function localTime(time: string, timeZone?: string): string {
  const date = new Date(time)
  if (isNaN(date.getTime())) return time
  const parts = new Intl.DateTimeFormat('en-US', {
    month: 'short', day: 'numeric', year: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit', fractionalSecondDigits: 3, hourCycle: 'h23',
    timeZoneName: 'short', timeZone,
  }).formatToParts(date)
  const part = (type: Intl.DateTimeFormatPartTypes) => parts.find(p => p.type === type)?.value ?? ''
  return `${part('month')} ${part('day')}, ${part('year')} at ${part('hour')}:${part('minute')}:${part('second')}.${part('fractionalSecond')} ${part('timeZoneName')}`
}

/** One line of a Log's JSON, as the inspector shows it. */
export interface JsonLine {
  depth: number
  key?: string
  /** What follows the key: a value as JSON, or a bracket. */
  text: string
  kind: 'string' | 'number' | 'bool' | 'null' | 'empty' | 'open' | 'close'
  /** Whether a comma ends the line. */
  comma: boolean
  /** On an opening bracket: the index of the line closing it, and how many entries it holds. */
  end?: number
  size?: number
  /** The Field whose value the line is (a built-in field or an Attribute), to add to the Query. */
  field?: string
  /** On an element of an array Field: the Field. */
  elementOf?: string
  /** The value the line holds whole, to copy. */
  value?: AttributeValue
}

/**
 * A Log's JSON (as inspectorJson writes it) one line each, so each line can
 * offer its Field to the Query and brackets can fold.
 */
export function jsonLines(log: Log): JsonLine[] {
  const lines: JsonLine[] = []
  const emit = (value: AttributeValue, depth: number, comma: boolean, at: Omit<JsonLine, 'depth' | 'text' | 'kind' | 'comma'>) => {
    if (value === null || typeof value !== 'object' || isEmpty(value)) {
      lines.push({ depth, ...at, text: JSON.stringify(value), kind: kindOf(value), comma, value })
      return
    }
    const array = Array.isArray(value)
    const entries: [string | undefined, AttributeValue][] = array
      ? value.map(v => [undefined, v as AttributeValue])
      : Object.entries(value as Record<string, AttributeValue>)
    const open = lines.length
    lines.push({ depth, ...at, text: array ? '[' : '{', kind: 'open', comma: false, size: entries.length, value })
    entries.forEach(([key, child], i) => {
      const last = i === entries.length - 1
      emit(child, depth + 1, !last, { key, ...childOf(depth, key, at.field) })
    })
    lines.push({ depth, text: array ? ']' : '}', kind: 'close', comma })
    lines[open].end = lines.length - 1
  }
  emit({ ...log, time: utcTime(log.time) }, 0, false, {})
  return lines
}

/**
 * What a line inside the document is: the Log's own keys and its Attributes
 * are Fields (but id, and the attributes object itself); an array Field's
 * elements are its elements.
 */
function childOf(depth: number, key: string | undefined, parentField: string | undefined): Pick<JsonLine, 'field' | 'elementOf'> {
  if (depth === 0) return key === 'id' || key === 'attributes' ? {} : { field: key }
  if (depth === 1 && parentField === undefined) return { field: key }
  if (parentField !== undefined && key === undefined) return { elementOf: parentField }
  return {}
}

function isEmpty(value: object): boolean {
  return Array.isArray(value) ? value.length === 0 : Object.keys(value).length === 0
}

function kindOf(value: AttributeValue): JsonLine['kind'] {
  if (value === null) return 'null'
  if (typeof value === 'string') return 'string'
  if (typeof value === 'number') return 'number'
  if (typeof value === 'boolean') return 'bool'
  return 'empty'
}

/** How far either side of a Log "Logs around this one" reads. */
const AROUND_MS = 60_000

/**
 * "Logs around this one": the Logs of the Log's Service, or of none when it
 * has none, from a minute before it to a minute after.
 */
export function aroundLog(log: Log): { builder: Builder; range: AbsoluteRange } {
  const time = new Date(log.time).getTime()
  const service: Chip = log.service ? { field: 'service', op: '==', value: log.service } : { field: 'service', op: 'missing', value: null }
  return {
    builder: { ...EMPTY_BUILDER, filters: [service] },
    range: { kind: 'absolute', from: new Date(time - AROUND_MS), to: new Date(time + AROUND_MS) },
  }
}

/**
 * When "Logs around this one" filled a page before reaching its Log, the
 * range that brings the Log into the first page: the same start, ending as
 * long after the Log as the Logs loaded took to fill half a page, so a busy
 * Service needs a run or two rather than a page per 200 newer Logs. Null when
 * that ends no sooner than the range does, so older pages load instead.
 */
export function narrowAround(range: AbsoluteRange, stream: Stream, log: Log): AbsoluteRange | null {
  const oldest = stream.logs.at(-1)
  if (!oldest) return null
  const end = range.to.getTime()
  const loadedMs = end - new Date(oldest.time).getTime()
  const afterMs = Math.ceil((loadedMs * PAGE_LOGS) / 2 / stream.logs.length)
  // A Log's time may have more digits than a millisecond, so end after its millisecond.
  const to = new Date(log.time).getTime() + Math.max(afterMs, 1)
  return to < end ? { kind: 'absolute', from: range.from, to: new Date(to) } : null
}

/** Why the stream of "Logs around this one" ended without its Log. */
export function aroundMissed(stream: Stream): string {
  return streamEnd(stream) === 'capped'
    ? `This Log is more than ${MAX_STREAM_LOGS.toLocaleString('en-US')} Logs into the stream, so it can't be shown. Pick a shorter range around it.`
    : "This Log isn't in the stream any more: it may have left the Project's Retention."
}
