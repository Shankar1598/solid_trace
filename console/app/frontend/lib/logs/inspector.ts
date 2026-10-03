// What the inspector shows of one Log: its Fields, each a tag, and its JSON,
// with the raw time in UTC so it can be shared exactly.
import type { AttributeValue, Log } from './stream'

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
